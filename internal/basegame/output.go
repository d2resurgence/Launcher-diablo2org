package basegame

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/diablo2org/launcher/internal/cdkey"
)

// Stage is what an install is doing.
type Stage string

const (
	// StageDownload fetches a product's payload from Blizzard's CDN.
	StageDownload Stage = "download"
	// StageInstall builds the game folder from the payload.
	StageInstall Stage = "install"
	// StagePatch patches the game to the chosen version.
	StagePatch Stage = "patch"
)

// Progress is an install's progress. legacy-dl installs the classic game,
// D2DV, then the expansion, D2XP, over it.
type Progress struct {
	Product string `json:"product"`
	Stage   Stage  `json:"stage"`
	// Done and Total count download pieces; both are 0 in other stages.
	Done  int `json:"done"`
	Total int `json:"total"`
}

// legacy-dl has no machine-readable progress. Off a terminal it prints a
// plain counter and a few landmark lines; these match them, as of v0.2.0.
var (
	productLine = regexp.MustCompile(`^=== ([A-Z0-9]{4}) ===$`)
	piecesLine  = regexp.MustCompile(`^(\d+)/(\d+) pieces$`)
	errorLine   = regexp.MustCompile(`^error: (\w+)$`)
)

// output reads legacy-dl's stdout and stderr: it reports progress, logs
// everything else with the keys taken out, and remembers what explains a
// failure. exec writes to it from one goroutine at a time.
type output struct {
	progress func(Progress)
	log      func(string)
	keys     []string

	mu       sync.Mutex
	partial  []byte
	cur      Progress
	errName  string
	noPatch  bool
	tail     []string
	warnings []string
}

func newOutput(progress func(Progress), log func(string), keys ...string) *output {
	return &output{progress: progress, log: log, keys: keys}
}

// Write splits what legacy-dl writes into lines. The counter is redrawn with
// a carriage return, so that ends a line too.
func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.partial = append(o.partial, p...)
	for {
		i := bytes.IndexAny(o.partial, "\r\n")
		if i < 0 {
			break
		}
		o.line(string(o.partial[:i]))
		o.partial = o.partial[i+1:]
	}
	return len(p), nil
}

// Flush handles a last line with no line end.
func (o *output) Flush() {
	o.mu.Lock()
	defer o.mu.Unlock()

	if len(o.partial) > 0 {
		o.line(string(o.partial))
		o.partial = nil
	}
}

func (o *output) line(raw string) {
	line := strings.TrimSpace(string(cdkey.Redact([]byte(raw), o.keys...)))
	if line == "" {
		return
	}

	if m := piecesLine.FindStringSubmatch(line); m != nil {
		done, _ := strconv.Atoi(m[1])
		total, _ := strconv.Atoi(m[2])
		o.report(Progress{Product: o.cur.Product, Stage: StageDownload, Done: done, Total: total})
		// The counter isn't worth a log line each time.
		return
	}

	o.log(line)
	o.tail = append(o.tail, line)
	if len(o.tail) > 5 {
		o.tail = o.tail[1:]
	}

	switch {
	case productLine.MatchString(line):
		o.report(Progress{Product: productLine.FindStringSubmatch(line)[1], Stage: StageDownload})
	case strings.HasPrefix(line, "installing ") && strings.Contains(line, " operations "):
		o.report(Progress{Product: o.cur.Product, Stage: StageInstall})
	case strings.HasPrefix(line, "patching to "):
		o.report(Progress{Product: o.cur.Product, Stage: StagePatch})
	case strings.HasPrefix(line, "no patch archive for "):
		o.noPatch = true
	case strings.HasPrefix(line, "!! the ") && strings.Contains(line, " key "):
		o.warnings = append(o.warnings, strings.TrimPrefix(line, "!! "))
	case errorLine.MatchString(line):
		o.errName = errorLine.FindStringSubmatch(line)[1]
	}
}

func (o *output) report(p Progress) {
	o.cur = p
	if o.progress != nil {
		o.progress(p)
	}
}

// err explains a failed run.
func (o *output) err(runErr error) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	switch {
	case o.noPatch || o.errName == "NoSuchVersion":
		return ErrPatchSource
	case o.errName == "AllPiecesFailed" || o.errName == "Incomplete" ||
		o.errName == "HttpStatus" || o.errName == "NoStub":
		return ErrDownload
	}

	detail := strings.Join(o.tail, "\n")
	if detail == "" {
		return fmt.Errorf("installing Diablo II failed: %w", runErr)
	}
	return fmt.Errorf("installing Diablo II failed: %w\n%s", runErr, detail)
}

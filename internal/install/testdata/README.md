`d2sfx-114.mpq` and `d2sfx-113.mpq` are tiny made-up archives, built by the
archive builder in `internal/mpq/mpq_test.go`. They hold one sound member
and an `(attributes)` member: in the 1.14 format (CRC32, FILETIME and MD5
columns, zlib-compressed) and the pre-1.14 one (CRC32 and FILETIME, stored).
Neither holds any of Blizzard's data.

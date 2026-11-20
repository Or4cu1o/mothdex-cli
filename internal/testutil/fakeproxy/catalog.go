package fakeproxy

// Status of a catalog case.
type Status string

// Case statuses. Every case starts Pending until the downloader (T-17,
// Forja) exists; whoever wires a case to a real test flips it to Covered and
// fills Test with the test function name.
const (
	Pending Status = "pending"
	Covered Status = "covered"
)

// Case maps one numbered test case of the T-16 threat model, section 8, to the
// fake behavior it needs and what it must assert.
type Case struct {
	ID     int
	Group  string
	Title  string
	Needs  string // fakeproxy behavior, or another test double the case needs
	Assert string
	Status Status
	Test   string // test function once Covered
}

const (
	grpSanitize = "sanitize"
	grpDest     = "destination"
	grpExists   = "overwrite"
	grpNet      = "network"
	grpDisk     = "disk"
	grpVerify   = "verify"
	grpRefresh  = "refresh-covers"
	grpOutput   = "output"
	grpInstall  = "installer"
)

// Catalog lists the 52 cases of T-16 section 8 (docs/security/
// T-16-threat-model-downloader.md). Needs names the Step constructor or the
// other double: "none" is a pure function, "fakefs" an os.Root-based fake file
// system with fault injection, "realfs" a real temp dir, "fakeresolver" a fake
// DNS resolver, "[W]"/"[L]" the CI runner that must execute it.
func Catalog() []Case {
	return []Case{
		{1, grpSanitize, "Traversal and absolute names", "none", "Sanitize keeps only the base name for ../../etc/passwd, ..\\..\\x.zip, /abs/x.zip, C:\\x.zip, UNC and dir/sub/x.zip; never escapes", Pending, ""},
		{2, grpSanitize, "Windows reserved names", "none", "CON, con.zip, NUL.tar.gz, COM1.bin, COM1 superscript, LPT9, CONIN$, 'aux .zip' get a _ prefix", Pending, ""},
		{3, grpSanitize, "Trailing dots and spaces", "none", "game.zip. / game.zip<space> / game.zip. . . lose the trailing dots and spaces", Pending, ""},
		{4, grpSanitize, "Colon and forbidden characters", "none", "game.zip:evil, x::$DATA, a<b>c|d?e*f\"g.zip leave no : or forbidden character", Pending, ""},
		{5, grpSanitize, "Control characters", "none", "NUL, ESC[31m, DEL and newline inside the name are removed", Pending, ""},
		{6, grpSanitize, "Bidi, zero-width and fullwidth", "none", "U+202E and U+200B removed; fullwidth slash and colon give no separator or colon semantics", Pending, ""},
		{7, grpSanitize, "NFD equals NFC", "none", "decomposed and precomposed Pokemon with acute e give identical output", Pending, ""},
		{8, grpSanitize, "Leading dot, dash, space", "none", ".bashrc and -rf.zip lose the leading characters", Pending, ""},
		{9, grpSanitize, "Length limit", "none", "300-byte ASCII, 3-byte-rune and 4-byte-emoji names give <= 200 bytes, valid UTF-8, extension kept", Pending, ""},
		{10, grpSanitize, "Empty and dot-only fallback", "none", "empty, '.', '..', '...', spaces, lone U+202E fall back to slug.format; slug ../x and format zip/../../x stay safe; empty slug gives download-<jobID>", Pending, ""},
		{11, grpSanitize, "Executable deny-list", "none", "x.sh x.SH x.zip.sh x.desktop x.AppImage x.exe x.bat x.lnk x.ps1 give ErrUnsafeName; x.bin .cue .iso .chd .zip .7z pass; --allow-executable allows the first group", Pending, ""},
		{12, grpSanitize, "Sanitize property and fuzz", "none (testing.F)", "for random bytes the section 5.9 invariants hold and Sanitize is idempotent", Pending, ""},

		{13, grpDest, "Platform id injection", "OK (any)", "../../system, ps1/../.., PS1, 'ps1 ', empty and unknown zzz in per_platform_id fail before any file is created", Pending, ""},
		{14, grpDest, "profile.folders traversal", "none", "folders.md = ../x or a/b gives a config/profile load error (exit 2)", Pending, ""},
		{15, grpDest, "Launcher folder as destination", "none", "-o or folder map equal to the launcher dir (ports, ports_scripts, windows) is rejected by loader or Preflight", Pending, ""},
		{16, grpDest, "[L] Symlinked destdir", "realfs [L]", "symlink to a dir outside the root gives dest_outside_root; -o with the real path succeeds", Pending, ""},
		{17, grpDest, "[W] Junction destdir", "realfs [W]", "mklink /J to a dir outside the root gives dest_outside_root (guards F7)", Pending, ""},
		{18, grpDest, "Symlink at the final name", "OK + realfs", "existing symlink to /tmp/victim: victim unchanged under skip, rename and overwrite; overwrite fails because target is not a regular file", Pending, ""},
		{19, grpDest, "Pre-planted temp name", "OK + realfs", "symlink or file at .<jobID>.mothdex-tmp makes the job fail or pick a fresh jobID; nothing outside destdir is touched", Pending, ""},

		{20, grpExists, "on_exists skip", "OK", "existing file: Skipped, bytes identical, no .part left", Pending, ""},
		{21, grpExists, "Race at finalize", "OK + fakefs hook", "a second writer creates the final name after Preflight and before finalize: existing file kept, on_exists re-applied", Pending, ""},
		{22, grpExists, "overwrite needs --force", "OK", "on_exists overwrite from config or MOTHDEX_DOWNLOAD_ON_EXISTS without --force is rejected (exit 2); with --force replaced atomically", Pending, ""},
		{23, grpExists, "rename exhausts at 99", "OK + realfs", "name.zip .. name (99).zip present: Failed(exists), no 100th file", Pending, ""},
		{24, grpExists, "Batch case and colon collisions", "OK on two paths", "Game.zip vs game.zip, A:B.zip vs A_B.zip: second job not silently overwritten; both outcomes in history", Pending, ""},
		{25, grpExists, "[W]/[L] case-insensitive fs", "OK + realfs [W]", "existing GAME.ZIP, download game.zip with skip gives Skipped", Pending, ""},

		{26, grpNet, "URL policy table", "none (Connections() == 0)", "http, :8443, userinfo, evil host, 127.0.0.1, [::1], suffix host, IDN give untrusted_url with no connection; trailing dot and upper case accepted", Pending, ""},
		{27, grpNet, "Redirects", "Redirect(302, allowed) and Redirect(302, evil)", "both Failed(untrusted_url), not retried (Requests() length 1)", Pending, ""},
		{28, grpNet, "Off-proxy link ranking", "none", "archive.org link excluded from ranking; download --link n on it gives untrusted_url with a clear message", Pending, ""},
		{29, grpNet, "Poisoned cache entry", "none", "cached entry with malicious filename/url gets the same rejections as from the network", Pending, ""},
		{30, grpNet, "Gzip despite identity", "GzipDespiteIdentity", "Failed(integrity), nothing finalized; Requests()[0] carried Accept-Encoding: identity", Pending, ""},
		{31, grpNet, "Length mismatch and oversize", "LengthMismatch, Oversized, MaliciousDisposition", "CL != link.size gives size_mismatch, not retried; no CL with size+1 bytes aborts at byte size+1 and removes .part; disposition name ignored", Pending, ""},
		{32, grpNet, "Hostile sizes", "none (Connections() == 0)", "link.size 0, -1, MaxInt64, > max_file_size give Failed in Preflight, no connection", Pending, ""},
		{33, grpNet, "Self-signed certificate", "Transport() without the CA", "TLS failure, retried as network error, never succeeds; no code path disables verification", Pending, ""},

		{34, grpDisk, "ENOSPC mid-stream", "OK + fakefs ENOSPC", "Failed(disk_full), not retried, .part gone, reservation released", Pending, ""},
		{35, grpDisk, "Reservation across jobs", "OK x2 + fakefs free space", "two jobs of 60% free space at concurrency 2: second fails in Preflight (disk_full)", Pending, ""},
		{36, grpDisk, "Two filesystems", "OK + fakefs device ids", "enough space in temp, not in dest: disk_full in Preflight", Pending, ""},
		{37, grpDisk, "EXDEV copy path", "OK + fakefs EXDEV + fsync spy", "copy to staging, fsync called, rename within destdir; crash after copy leaves only staging, never a partial final name", Pending, ""},
		{38, grpDisk, "Cancel in Transferring and Verifying", "StallMidStream + fakeclock", "staging removed, Canceled, one history record", Pending, ""},
		{39, grpDisk, "Startup cleanup", "realfs + fakeclock mtimes", ".part at 25 h deleted, 23 h kept, symlink not followed, notes.part kept, dir x.part kept, journal destdir staging deleted when old", Pending, ""},
		{40, grpDisk, "Retry reuses the fd", "Truncate then OK + fakefs hook", "path swapped for a symlink during RetryWait: symlink target never written", Pending, ""},

		{41, grpVerify, "Zip bombs", "OK(zip bytes)", "member data over declared size gives integrity; overlap bomb stops at cumulative cap; 70000 empty members stop at member cap; bounded bytes read", Pending, ""},
		{42, grpVerify, "Cancel during zip CRC", "OK(zip bytes) + ctx", "returns promptly, Canceled", Pending, ""},
		{43, grpVerify, "Magic-byte detection", "OK(PK 05 06 bytes), OK(random .zip)", "CRC step skipped or failed as specified; history records crc skipped", Pending, ""},

		{44, grpRefresh, "Refresh URL policy", "none", "http://localhost, 127.0.0.1.nip.io, 192.168.x.x, https://127.0.0.1 rejected at profile load; http://127.0.0.1/reloadgames and [::1] accepted", Pending, ""},
		{45, grpRefresh, "Refresh hardening", "Redirect, Oversized(10 MiB), StallBeforeHeaders + fakeclock", "no redirect followed, <= 64 KiB read, returns within 3 s of fake time, warning only, HTTP_PROXY unused", Pending, ""},
		{46, grpRefresh, "Refresh once per batch", "OK", "called once when >= 1 Done; never when all Skipped/Failed", Pending, ""},
		{47, grpRefresh, "Cover SSRF", "Redirect + fakeresolver", "http, 192.168.0.1, DNS to 127.0.0.1, redirect to private IP: not fetched; job still Done", Pending, ""},
		{48, grpRefresh, "Cover content checks", "OK(html/svg bytes).With(Content-Type, image/png); OK(png header 60000x60000)", "rejected, not stored", Pending, ""},

		{49, grpOutput, "Terminal escapes", "none", "title/filename with OSC 52 and ESC[2J: human output has no ESC byte; --json escapes it", Pending, ""},
		{50, grpOutput, "CSV formula guard", "none", "filename =HYPERLINK(...) exported cell begins with a quote", Pending, ""},

		{51, grpInstall, "Launcher path injection (T-21)", "none", "bin_dir with a space and with $(id): refused or quoted so that sh -n passes and the literal path runs", Pending, ""},
		{52, grpInstall, "Launcher permissions (T-21)", "realfs [L]", "launcher mode 0755, owned by installer user, dir not world-writable; reinstall keeps a user-modified launcher without the marker", Pending, ""},
	}
}

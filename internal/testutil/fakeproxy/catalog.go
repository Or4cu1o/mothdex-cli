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
	ID       int
	Group    string
	Title    string
	Needs    string // fakeproxy behavior, or another test double the case needs
	Assert   string
	Runner   string // CI runner that must execute it: "L", "W", "W+L"; empty means any
	Provider string // who delivers a double this package does not contain yet
	Status   Status
	Test     string // test function once Covered
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
		{ID: 1, Group: grpSanitize, Title: "Traversal and absolute names", Needs: "none", Assert: "Sanitize keeps only the base name for ../../etc/passwd, ..\\..\\x.zip, /abs/x.zip, C:\\x.zip, UNC and dir/sub/x.zip; never escapes", Status: Covered, Test: "TestSanitizeTable"},
		{ID: 2, Group: grpSanitize, Title: "Windows reserved names", Needs: "none", Assert: "CON, con.zip, NUL.tar.gz, COM1.bin, COM1 superscript, LPT9, CONIN$, 'aux .zip' get a _ prefix", Status: Covered, Test: "TestSanitizeTable"},
		{ID: 3, Group: grpSanitize, Title: "Trailing dots and spaces", Needs: "none", Assert: "game.zip. / game.zip<space> / game.zip. . . lose the trailing dots and spaces", Status: Covered, Test: "TestSanitizeTable"},
		{ID: 4, Group: grpSanitize, Title: "Colon and forbidden characters", Needs: "none", Assert: "game.zip:evil, x::$DATA, a<b>c|d?e*f\"g.zip leave no : or forbidden character", Status: Covered, Test: "TestSanitizeTable"},
		{ID: 5, Group: grpSanitize, Title: "Control characters", Needs: "none", Assert: "NUL, ESC[31m, DEL and newline inside the name are removed", Status: Covered, Test: "TestSanitizeTable"},
		{ID: 6, Group: grpSanitize, Title: "Bidi, zero-width and fullwidth", Needs: "none", Assert: "U+202E and U+200B removed; fullwidth slash and colon give no separator or colon semantics", Status: Covered, Test: "TestSanitizeTable"},
		{ID: 7, Group: grpSanitize, Title: "NFD equals NFC", Needs: "none", Assert: "decomposed and precomposed Pokemon with acute e give identical output", Status: Covered, Test: "TestSanitizeTable"},
		{ID: 8, Group: grpSanitize, Title: "Leading dot, dash, space", Needs: "none", Assert: ".bashrc and -rf.zip lose the leading characters", Status: Covered, Test: "TestSanitizeTable"},
		{ID: 9, Group: grpSanitize, Title: "Length limit", Needs: "none", Assert: "300-byte ASCII, 3-byte-rune and 4-byte-emoji names give <= 200 bytes, valid UTF-8, extension kept", Status: Covered, Test: "TestSanitizeLength"},
		{ID: 10, Group: grpSanitize, Title: "Empty and dot-only fallback", Needs: "none", Assert: "empty, '.', '..', '...', spaces, lone U+202E fall back to slug.format; slug ../x and format zip/../../x stay safe; empty slug gives download-<jobID>", Status: Covered, Test: "TestSanitizeTable"},
		{ID: 11, Group: grpSanitize, Title: "Executable deny-list", Needs: "none", Assert: "x.sh x.SH x.zip.sh x.desktop x.AppImage x.exe x.bat x.lnk x.ps1 give ErrUnsafeName; x.bin .cue .iso .chd .zip .7z pass; --allow-executable allows the first group", Status: Covered, Test: "TestSanitizeTable"},
		{ID: 12, Group: grpSanitize, Title: "Sanitize property and fuzz", Needs: "none (testing.F)", Assert: "for random bytes the section 5.9 invariants hold and Sanitize is idempotent", Status: Covered, Test: "TestCase12SanitizeInvariantsOnRandomInput"},

		{ID: 13, Group: grpDest, Title: "Platform id injection", Needs: "OK (any)", Assert: "../../system, ps1/../.., PS1, 'ps1 ', empty and unknown zzz in per_platform_id fail before any file is created", Status: Covered, Test: "TestCase13PlatformIDInjection"},
		{ID: 14, Group: grpDest, Title: "profile.folders traversal", Needs: "none", Assert: "folders.md = ../x or a/b gives a config/profile load error (exit 2)", Status: Pending},
		{ID: 15, Group: grpDest, Title: "Launcher folder as destination", Needs: "none", Assert: "-o or folder map equal to the launcher dir (ports, ports_scripts, windows) is rejected by loader or Preflight", Status: Covered, Test: "TestCase15LauncherFolderAsDestination"},
		{ID: 16, Group: grpDest, Title: "Symlinked destdir", Needs: "realfs", Assert: "symlink to a dir outside the root gives dest_outside_root; -o with the real path succeeds", Status: Covered, Test: "TestCase16SymlinkedDestination", Runner: "L", Provider: "realfs: plain t.TempDir, no extra double"},
		{ID: 17, Group: grpDest, Title: "Junction destdir", Needs: "realfs", Assert: "mklink /J to a dir outside the root gives dest_outside_root (guards F7)", Status: Covered, Test: "TestCase17JunctionDestination", Runner: "W", Provider: "realfs: plain t.TempDir, no extra double"},
		{ID: 18, Group: grpDest, Title: "Symlink at the final name", Needs: "OK + realfs", Assert: "existing symlink to /tmp/victim: victim unchanged under skip, rename and overwrite; overwrite fails because target is not a regular file", Status: Covered, Test: "TestCase18SymlinkAtTheFinalName", Provider: "realfs: plain t.TempDir, no extra double"},
		{ID: 19, Group: grpDest, Title: "Pre-planted temp name", Needs: "OK + realfs", Assert: "symlink or file at .<jobID>.mothdex-tmp makes the job fail or pick a fresh jobID; nothing outside destdir is touched", Status: Covered, Test: "TestCase19PreplantedTemporaryName", Provider: "realfs: plain t.TempDir, no extra double"},

		{ID: 20, Group: grpExists, Title: "on_exists skip", Needs: "OK", Assert: "existing file: Skipped, bytes identical, no .part left", Status: Covered, Test: "TestCase20SkipLeavesNoStagingFile"},
		{ID: 21, Group: grpExists, Title: "Race at finalize", Needs: "OK + fakefs hook", Assert: "a second writer creates the final name after Preflight and before finalize: existing file kept, on_exists re-applied", Status: Covered, Test: "TestCase21RaceAtFinalize", Provider: "fakefs: T-17 (Forja), needs the os.Root FS and Finalizer interfaces"},
		{ID: 22, Group: grpExists, Title: "overwrite needs --force", Needs: "OK", Assert: "on_exists overwrite from config or MOTHDEX_DOWNLOAD_ON_EXISTS without --force is rejected (exit 2); with --force replaced atomically", Status: Pending},
		{ID: 23, Group: grpExists, Title: "rename exhausts at 99", Needs: "OK + realfs", Assert: "name.zip .. name (99).zip present: the 100th name fails and no 100th file is created", Status: Covered, Test: "TestCase23RenameExhaustsAt99", Provider: "realfs: plain t.TempDir, no extra double"},
		{ID: 24, Group: grpExists, Title: "Batch case and colon collisions", Needs: "OK on two paths", Assert: "Game.zip vs game.zip, A:B.zip vs A_B.zip: second job not silently overwritten; both outcomes in history", Status: Covered, Test: "TestCase24BatchNameCollisions"},
		{ID: 25, Group: grpExists, Title: "[W]/case-insensitive fs", Needs: "OK + realfs", Assert: "existing GAME.ZIP, download game.zip with skip gives Skipped", Status: Covered, Test: "TestCase25CaseInsensitiveFileSystem", Runner: "W+L", Provider: "realfs: plain t.TempDir, no extra double"},

		{ID: 26, Group: grpNet, Title: "URL policy table", Needs: "none (Connections() == 0)", Assert: "http, :8443, userinfo, evil host, 127.0.0.1, [::1], suffix host, IDN give untrusted_url with no connection; trailing dot and upper case accepted", Status: Covered, Test: "TestCase26URLPolicyThroughTheDownloader"},
		{ID: 27, Group: grpNet, Title: "Redirects", Needs: "Redirect(302, allowed) and Redirect(302, evil)", Assert: "both Failed(untrusted_url), not retried (Requests() length 1)", Status: Covered, Test: "TestCase27RedirectsAreNeverFollowedNorRetried"},
		{ID: 28, Group: grpNet, Title: "Off-proxy link ranking", Needs: "none", Assert: "archive.org link excluded from ranking; download --link n on it gives untrusted_url with a clear message", Status: Pending},
		{ID: 29, Group: grpNet, Title: "Poisoned cache entry", Needs: "none", Assert: "cached entry with malicious filename/url gets the same rejections as from the network", Status: Covered, Test: "TestCase29HostileLinkFieldsGetTheSameRejections"},
		{ID: 30, Group: grpNet, Title: "Gzip despite identity", Needs: "GzipDespiteIdentity", Assert: "Failed(integrity), nothing finalized; Requests()[0] carried Accept-Encoding: identity", Status: Covered, Test: "TestCase30GzipDespiteIdentityIsAnIntegrityFailure"},
		{ID: 31, Group: grpNet, Title: "Length mismatch and oversize", Needs: "LengthMismatch, Oversized, MaliciousDisposition", Assert: "CL != link.size gives size_mismatch, not retried; no CL with size+1 bytes aborts at byte size+1 and removes .part; disposition name ignored", Status: Covered, Test: "TestCase31LengthMismatchOversizeAndDisposition"},
		{ID: 32, Group: grpNet, Title: "Hostile sizes", Needs: "none (Connections() == 0)", Assert: "link.size 0, -1, MaxInt64, > max_file_size give Failed in Preflight, no connection", Status: Covered, Test: "TestCase32HostileSizesFailBeforeAnyConnection"},
		{ID: 33, Group: grpNet, Title: "Self-signed certificate", Needs: "UntrustedClient()", Assert: "TLS verification fails and the file is never finalized; no code path disables verification (assert by behavior, no retry policy implied)", Status: Covered, Test: "TestCase33SelfSignedCertificateIsNeverFinalized"},

		{ID: 34, Group: grpDisk, Title: "ENOSPC mid-stream", Needs: "OK + fakefs ENOSPC", Assert: "Failed(disk_full), not retried, .part gone, reservation released", Status: Covered, Test: "TestCase34DiskFullMidStream", Provider: "fakefs: T-17 (Forja), needs the os.Root FS and Finalizer interfaces"},
		{ID: 35, Group: grpDisk, Title: "Reservation across jobs", Needs: "OK x2 + fakefs free space", Assert: "two jobs of 60% free space at concurrency 2: second fails in Preflight (disk_full)", Status: Covered, Test: "TestCase35ReservationAcrossJobs", Provider: "fakefs: T-17 (Forja), needs the os.Root FS and Finalizer interfaces"},
		{ID: 36, Group: grpDisk, Title: "Two filesystems", Needs: "OK + fakefs device ids", Assert: "enough space in temp, not in dest: disk_full in Preflight", Status: Covered, Test: "TestCase36TwoFileSystems", Provider: "fakefs: T-17 (Forja), needs the os.Root FS and Finalizer interfaces"},
		{ID: 37, Group: grpDisk, Title: "EXDEV copy path", Needs: "OK + fakefs EXDEV + fsync spy", Assert: "copy to staging, fsync called, rename within destdir; crash after copy leaves only staging, never a partial final name", Status: Covered, Test: "TestCase37CrossDeviceCopyPath", Provider: "fakefs: T-17 (Forja), needs the os.Root FS and Finalizer interfaces"},
		{ID: 38, Group: grpDisk, Title: "Cancel in Transferring and Verifying", Needs: "StallMidStream + fakeclock", Assert: "staging removed, Canceled, one history record", Status: Covered, Test: "TestCase38CancelInTransferringAndVerifying"},
		{ID: 39, Group: grpDisk, Title: "Startup cleanup", Needs: "realfs + fakeclock mtimes", Assert: ".part at 25 h deleted, 23 h kept, symlink not followed, notes.part kept, dir x.part kept, journal destdir staging deleted when old", Status: Covered, Test: "TestCase39StartupCleanup", Provider: "realfs: plain t.TempDir, no extra double"},
		{ID: 40, Group: grpDisk, Title: "Retry reuses the fd", Needs: "Truncate then OK + fakefs hook", Assert: "path swapped for a symlink during RetryWait: symlink target never written", Status: Covered, Test: "TestCase40RetryNeverWritesThroughASwappedPath", Provider: "fakefs: T-17 (Forja), needs the os.Root FS and Finalizer interfaces"},

		{ID: 41, Group: grpVerify, Title: "Zip bombs", Needs: "OK(zip bytes)", Assert: "member data over declared size gives integrity; overlap bomb stops at cumulative cap; 70000 empty members stop at member cap; bounded bytes read", Status: Covered, Test: "TestCase41ZipBombs"},
		{ID: 42, Group: grpVerify, Title: "Cancel during zip CRC", Needs: "OK(zip bytes) + ctx", Assert: "returns promptly, Canceled", Status: Covered, Test: "TestCase42CancelDuringZipCRC"},
		{ID: 43, Group: grpVerify, Title: "Magic-byte detection", Needs: "OK(PK 05 06 bytes), OK(random .zip)", Assert: "CRC step skipped or failed as specified; history records crc skipped", Status: Covered, Test: "TestCase43MagicByteDetection"},

		{ID: 44, Group: grpRefresh, Title: "Refresh URL policy", Needs: "none", Assert: "http://localhost, 127.0.0.1.nip.io, 192.168.x.x, https://127.0.0.1 rejected at profile load; http://127.0.0.1/reloadgames and [::1] accepted", Status: Pending},
		{ID: 45, Group: grpRefresh, Title: "Refresh hardening", Needs: "Redirect, Oversized(10 MiB), StallBeforeHeaders + fakeclock", Assert: "no redirect followed, <= 64 KiB read, returns within 3 s of fake time, warning only, HTTP_PROXY unused", Status: Pending},
		{ID: 46, Group: grpRefresh, Title: "Refresh once per batch", Needs: "OK", Assert: "called once when >= 1 Done; never when all Skipped/Failed", Status: Pending},
		{ID: 47, Group: grpRefresh, Title: "Cover SSRF", Needs: "Redirect + fakeresolver", Assert: "http, 192.168.0.1, DNS to 127.0.0.1, redirect to private IP: not fetched; job still Done", Status: Pending, Provider: "fakeresolver: T-17 (Forja), needs the cover-fetch resolver hook"},
		{ID: 48, Group: grpRefresh, Title: "Cover content checks", Needs: "OK(html/svg bytes).With(Content-Type, image/png); OK(png header 60000x60000)", Assert: "rejected, not stored", Status: Pending},

		{ID: 49, Group: grpOutput, Title: "Terminal escapes", Needs: "none", Assert: "title/filename with OSC 52 and ESC[2J: human output has no ESC byte; --json escapes it", Status: Pending},
		{ID: 50, Group: grpOutput, Title: "CSV formula guard", Needs: "none", Assert: "filename =HYPERLINK(...) exported cell begins with a quote", Status: Pending},

		{ID: 51, Group: grpInstall, Title: "Launcher path injection (T-21)", Needs: "none", Assert: "bin_dir with a space and with $(id): refused or quoted so that sh -n passes and the literal path runs", Status: Pending},
		{ID: 52, Group: grpInstall, Title: "Launcher permissions (T-21)", Needs: "realfs", Assert: "launcher mode 0755, owned by installer user, dir not world-writable; reinstall keeps a user-modified launcher without the marker", Status: Pending, Provider: "realfs: plain t.TempDir, no extra double"},
	}
}

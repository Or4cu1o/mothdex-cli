//go:build gui && linux

package sdl

import "testing"

// The line of /proc/<pid>/maps that maps SDL2: the path comes after the address, permissions, offset, device and inode,
// may hold spaces, and may end with "(deleted)" when the file was replaced after it was mapped.
func TestSDLPathInMapsLine(t *testing.T) {
	tests := []struct {
		name, line, want string
	}{
		{"the library", "7f3a5c000000-7f3a5c200000 r-xp 00000000 08:01 1234567    /usr/lib/x86_64-linux-gnu/libSDL2-2.0.so.0.3200.10", "/usr/lib/x86_64-linux-gnu/libSDL2-2.0.so.0.3200.10"},
		{"a path with a space", "7f3a5c000000-7f3a5c200000 r--p 00000000 08:01 1234567 /media/my games/lib/libSDL2-2.0.so.0", "/media/my games/lib/libSDL2-2.0.so.0"},
		{"replaced after loading", "7f3a5c000000-7f3a5c200000 r--p 00000000 08:01 1234567 /emuelec/lib/libSDL2-2.0.so.0 (deleted)", "/emuelec/lib/libSDL2-2.0.so.0"},
		{"another library", "7f3a5c000000-7f3a5c200000 r-xp 00000000 08:01 1234567 /usr/lib/libSDL2_image-2.0.so.0", ""},
		{"an anonymous mapping", "7f3a5c000000-7f3a5c200000 rw-p 00000000 00:00 0", ""},
		{"a heap line", "55d0c1a00000-55d0c1a21000 rw-p 00000000 00:00 0                          [heap]", ""},
	}
	for _, tc := range tests {
		if got := sdlPathInMapsLine(tc.line); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

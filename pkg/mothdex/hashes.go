package mothdex

import "strings"

// HashScope says what a Hashes value was computed over.
type HashScope string

// Known hash scopes.
const (
	HashScopeFile    HashScope = "file"    // the downloaded file
	HashScopeContent HashScope = "content" // the ROM inside the archive
)

// Hashes is the proposed optional upstream field. Hex strings are lowercased.
type Hashes struct {
	CRC32 string    `json:"crc32,omitempty"`
	MD5   string    `json:"md5,omitempty"`
	SHA1  string    `json:"sha1,omitempty"`
	Scope HashScope `json:"scope"`
}

// normalizeHashes lowercases the digests and drops values with an unknown scope.
func normalizeHashes(h *Hashes) *Hashes {
	if h == nil {
		return nil
	}
	if h.Scope != HashScopeFile && h.Scope != HashScopeContent {
		return nil
	}
	out := *h
	out.CRC32 = strings.ToLower(h.CRC32)
	out.MD5 = strings.ToLower(h.MD5)
	out.SHA1 = strings.ToLower(h.SHA1)
	return &out
}

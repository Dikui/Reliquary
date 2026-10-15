package types

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

// NewIngestJSONReader bounds transport allocation before JSON decoding. It also
// counts decoded content/markdown code points while bytes arrive, including
// JSON escapes and surrogate pairs. Images have a separate decoded byte limit.
func NewIngestJSONReader(reader io.Reader, maxBytes int64) io.Reader {
	return &ingestJSONReader{reader: reader, remaining: maxBytes, maximum: maxBytes}
}

type ingestJSONReader struct {
	reader                     io.Reader
	remaining                  int64
	maximum                    int64
	err                        error
	inString, escaped, counted bool
	key, token                 string
	unicodeDigits              string
	unicodeLeft                int
	runes                      int
	highSurrogate              bool
	depth, itemsDepth          int
	itemBytes                  int64
	inItem                     bool
}

func (r *ingestJSONReader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	if int64(len(p)) > r.remaining+1 {
		p = p[:r.remaining+1]
	}
	// Small reads bound decoder lookahead and stop early on overlong bodies.
	if len(p) > 32768 {
		p = p[:32768]
	}
	n, err := r.reader.Read(p)
	for i, b := range p[:n] {
		r.remaining--
		if r.remaining < 0 {
			r.err = fmt.Errorf("JSON body exceeds the byte limit (%d bytes)", r.maximum)
			return i, r.err
		}
		if r.inItem {
			r.itemBytes++
			if r.itemBytes > MaxIngestItemBytes {
				r.err = fmt.Errorf("item JSON exceeds 64 MiB (%d bytes)", MaxIngestItemBytes)
				return i, r.err
			}
		}
		if !r.inString && r.itemsDepth > 0 && r.depth == r.itemsDepth && !r.inItem && b != ' ' && b != '\n' && b != '\r' && b != '\t' && b != ',' && b != ']' {
			r.inItem = true
			r.itemBytes = 1
		}
		if r.inString {
			if !r.counted && len(r.token) < 256 && (b != '"' || r.escaped || r.unicodeLeft > 0) {
				r.token += string(b)
			}
			if r.unicodeLeft > 0 {
				r.unicodeDigits += string(b)
				r.unicodeLeft--
				if r.unicodeLeft == 0 {
					v, e := strconv.ParseUint(r.unicodeDigits, 16, 16)
					if e == nil && r.counted {
						if v < 0xdc00 || v > 0xdfff || !r.highSurrogate {
							r.runes++
						}
						r.highSurrogate = v >= 0xd800 && v <= 0xdbff
					}
					r.unicodeDigits = ""
				}
			} else if r.escaped {
				r.escaped = false
				if b == 'u' {
					r.unicodeLeft = 4
				} else if r.counted {
					r.highSurrogate = false
					r.runes++
				}
			} else if b == '\\' {
				r.escaped = true
			} else if b == '"' {
				r.inString = false
				var decoded string
				if json.Unmarshal([]byte("\""+r.token+"\""), &decoded) == nil {
					r.token = decoded
				}
			} else {
				if r.counted && b&0xc0 != 0x80 {
					r.highSurrogate = false
					r.runes++
				}

			}
			if r.runes > MaxIngestContentRunes {
				r.err = fmt.Errorf("content exceeds 5000000 Unicode code points")
				return i, r.err
			}
		} else {
			switch b {
			case '"':
				r.inString = true
				r.counted = r.key == "content" || r.key == "markdown"
				r.key = ""
				r.token = ""
				r.runes = 0
				r.highSurrogate = false
			case ':':
				r.key = r.token
			case '{':
				r.depth++
				r.key = ""
				r.token = ""
			case '[':
				r.depth++
				if r.key == "items" {
					r.itemsDepth = r.depth
				}
				r.key = ""
				r.token = ""
			case '}', ']':
				r.depth--
				if r.inItem && r.depth == r.itemsDepth {
					r.inItem = false
				}
				if r.depth < r.itemsDepth {
					r.itemsDepth = 0
				}
				r.key = ""
				r.token = ""
			case ',':
				if r.depth == r.itemsDepth {
					r.inItem = false
				}
				r.key = ""
				r.token = ""
			}
		}
	}
	return n, err
}

package metadata

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrNotPNG は PNG シグネチャを持たないデータを読もうとしたときに返る。
var ErrNotPNG = errors.New("metadata: not a PNG file")

// parametersKey は WebUI が生成情報を書き込むテキストチャンクのキー。
const parametersKey = "parameters"

// maxTextChunk はテキストチャンクとして読み込む上限。
// これを超えるチャンクは生成情報ではないとみなして読み飛ばす。
const maxTextChunk = 16 << 20

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// Info は PNG から読み取った画像情報。
// Parameters は parameters チャンクの原文で、存在しなければ空文字となる。
type Info struct {
	Width      int
	Height     int
	Parameters string
}

// ReadFile はファイルから画像情報を読み取る。
func ReadFile(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	return ReadInfo(f)
}

// ReadInfo は PNG のチャンクを走査し、画像サイズと parameters を読み取る。
// parameters が存在しないことはエラーとしない。
func ReadInfo(r io.Reader) (Info, error) {
	sig := make([]byte, len(pngSignature))
	if _, err := io.ReadFull(r, sig); err != nil {
		return Info{}, fmt.Errorf("metadata: read signature: %w", err)
	}
	if !bytes.Equal(sig, pngSignature) {
		return Info{}, ErrNotPNG
	}

	var info Info
	header := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, header); err != nil {
			if errors.Is(err, io.EOF) {
				return info, nil
			}
			return info, fmt.Errorf("metadata: read chunk header: %w", err)
		}
		length := int64(binary.BigEndian.Uint32(header[:4]))
		typ := string(header[4:8])

		switch {
		case typ == "IEND":
			return info, nil
		case typ == "IHDR":
			data, err := readChunk(r, length)
			if err != nil {
				return info, err
			}
			if len(data) < 8 {
				return info, fmt.Errorf("metadata: IHDR too short")
			}
			info.Width = int(binary.BigEndian.Uint32(data[0:4]))
			info.Height = int(binary.BigEndian.Uint32(data[4:8]))
		case isTextChunk(typ) && length <= maxTextChunk:
			data, err := readChunk(r, length)
			if err != nil {
				return info, err
			}
			key, value, err := decodeText(typ, data)
			if err == nil && key == parametersKey && info.Parameters == "" {
				info.Parameters = value
			}
		default:
			if err := skip(r, length); err != nil {
				return info, fmt.Errorf("metadata: skip chunk %s: %w", typ, err)
			}
		}

		// CRC を読み飛ばす。
		if err := skip(r, 4); err != nil {
			return info, fmt.Errorf("metadata: skip crc: %w", err)
		}
	}
}

func isTextChunk(typ string) bool {
	return typ == "tEXt" || typ == "iTXt" || typ == "zTXt"
}

func readChunk(r io.Reader, length int64) ([]byte, error) {
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("metadata: read chunk body: %w", err)
	}
	return data, nil
}

// decodeText はテキスト系チャンクからキーと値を取り出す。
func decodeText(typ string, data []byte) (string, string, error) {
	key, rest, found := bytes.Cut(data, []byte{0})
	if !found {
		return "", "", fmt.Errorf("metadata: %s chunk has no keyword separator", typ)
	}

	switch typ {
	case "tEXt":
		return string(key), string(rest), nil

	case "zTXt":
		if len(rest) < 1 {
			return "", "", fmt.Errorf("metadata: zTXt chunk is truncated")
		}
		text, err := inflate(rest[1:])
		if err != nil {
			return "", "", err
		}
		return string(key), text, nil

	case "iTXt":
		// compression flag, compression method, language tag, translated keyword が続く。
		if len(rest) < 2 {
			return "", "", fmt.Errorf("metadata: iTXt chunk is truncated")
		}
		compressed := rest[0] != 0
		rest = rest[2:]
		_, rest, found = bytes.Cut(rest, []byte{0}) // language tag
		if !found {
			return "", "", fmt.Errorf("metadata: iTXt chunk has no language tag")
		}
		_, rest, found = bytes.Cut(rest, []byte{0}) // translated keyword
		if !found {
			return "", "", fmt.Errorf("metadata: iTXt chunk has no translated keyword")
		}
		if !compressed {
			return string(key), string(rest), nil
		}
		text, err := inflate(rest)
		if err != nil {
			return "", "", err
		}
		return string(key), text, nil
	}
	return "", "", fmt.Errorf("metadata: unsupported text chunk %s", typ)
}

func inflate(b []byte) (string, error) {
	zr, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("metadata: open zlib reader: %w", err)
	}
	defer zr.Close()
	text, err := io.ReadAll(io.LimitReader(zr, maxTextChunk))
	if err != nil {
		return "", fmt.Errorf("metadata: inflate text chunk: %w", err)
	}
	return string(text), nil
}

// skip は n バイト読み飛ばす。Seeker であれば読み込まずに進める。
func skip(r io.Reader, n int64) error {
	if s, ok := r.(io.Seeker); ok {
		_, err := s.Seek(n, io.SeekCurrent)
		return err
	}
	_, err := io.CopyN(io.Discard, r, n)
	return err
}

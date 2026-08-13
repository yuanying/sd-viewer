package metadata

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

// buildPNG は指定したチャンクを持つ最小構成の PNG バイト列を組み立てる。
func buildPNG(width, height int, chunks ...[]byte) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})

	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], uint32(width))
	binary.BigEndian.PutUint32(ihdr[4:], uint32(height))
	ihdr[8] = 8 // bit depth
	ihdr[9] = 2 // color type: truecolor
	writeChunk(&buf, "IHDR", ihdr)

	for _, c := range chunks {
		buf.Write(c)
	}

	writeChunk(&buf, "IDAT", bytes.Repeat([]byte{0}, 32))
	writeChunk(&buf, "IEND", nil)
	return buf.Bytes()
}

func writeChunk(buf *bytes.Buffer, typ string, data []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(data)))
	buf.Write(length[:])
	body := append([]byte(typ), data...)
	buf.Write(body)
	var crc [4]byte
	binary.BigEndian.PutUint32(crc[:], crc32.ChecksumIEEE(body))
	buf.Write(crc[:])
}

func chunk(typ string, data []byte) []byte {
	var buf bytes.Buffer
	writeChunk(&buf, typ, data)
	return buf.Bytes()
}

func textChunk(key, value string) []byte {
	data := append([]byte(key), 0)
	return chunk("tEXt", append(data, value...))
}

func itxtChunk(key, value string) []byte {
	var data []byte
	data = append(data, key...)
	data = append(data, 0) // null separator
	data = append(data, 0) // compression flag: uncompressed
	data = append(data, 0) // compression method
	data = append(data, 0) // language tag (empty) + null
	data = append(data, 0) // translated keyword (empty) + null
	data = append(data, value...)
	return chunk("iTXt", data)
}

func ztxtChunk(key, value string) []byte {
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	w.Write([]byte(value))
	w.Close()

	data := append([]byte(key), 0)
	data = append(data, 0) // compression method: deflate
	data = append(data, compressed.Bytes()...)
	return chunk("zTXt", data)
}

func TestReadInfo_PNGから画像サイズとparametersを読み取る(t *testing.T) {
	const sample = "1girl\nSteps: 20, Sampler: Euler a"

	// テキストチャンクを IDAT より後ろ（IEND の直前）に置いた PNG。
	base := buildPNG(64, 64)
	trailingText := append([]byte{}, base[:len(base)-len(chunk("IEND", nil))]...)
	trailingText = append(trailingText, textChunk("parameters", sample)...)
	trailingText = append(trailingText, chunk("IEND", nil)...)

	tests := []struct {
		name    string
		png     []byte
		want    Info
		wantErr error
	}{
		{
			name: "tEXt チャンクから読み取る",
			png:  buildPNG(896, 1152, textChunk("parameters", sample)),
			want: Info{Width: 896, Height: 1152, Parameters: sample},
		},
		{
			name: "非圧縮の iTXt チャンクから読み取る",
			png:  buildPNG(512, 512, itxtChunk("parameters", sample)),
			want: Info{Width: 512, Height: 512, Parameters: sample},
		},
		{
			name: "圧縮された zTXt チャンクから読み取る",
			png:  buildPNG(512, 512, ztxtChunk("parameters", sample)),
			want: Info{Width: 512, Height: 512, Parameters: sample},
		},
		{
			name: "parameters がなくても画像サイズは取れる",
			png:  buildPNG(64, 128),
			want: Info{Width: 64, Height: 128},
		},
		{
			name: "parameters 以外のテキストチャンクは無視する",
			png:  buildPNG(64, 64, textChunk("Software", "gimp"), textChunk("Comment", "hello")),
			want: Info{Width: 64, Height: 64},
		},
		{
			name: "IDAT より後ろにある parameters も読み取る",
			png:  trailingText,
			want: Info{Width: 64, Height: 64, Parameters: sample},
		},
		{
			name:    "PNG でないデータはエラーを返す",
			png:     []byte("this is not a png file at all"),
			wantErr: ErrNotPNG,
		},
		{
			name: "途中で切れたデータはエラーを返す",
			png:  buildPNG(64, 64, textChunk("parameters", sample))[:20],
			want: Info{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: PNG バイト列
			// When: 読み込む
			got, err := ReadInfo(bytes.NewReader(tt.png))

			// Then: 期待どおりの結果かエラーになる
			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
			case tt.want == (Info{}):
				if err == nil {
					t.Fatal("error = nil, want non-nil")
				}
			default:
				if err != nil {
					t.Fatalf("ReadInfo() error = %v", err)
				}
				if got != tt.want {
					t.Errorf("got = %#v, want %#v", got, tt.want)
				}
			}
		})
	}
}

func TestReadFile_ファイルからparametersを取り出す(t *testing.T) {
	tests := []struct {
		name    string
		write   bool
		content []byte
		want    string
		wantErr bool
	}{
		{
			name:    "parameters を持つファイルを読み取る",
			write:   true,
			content: buildPNG(896, 1152, textChunk("parameters", fullParameters)),
			want:    fullParameters,
		},
		{
			name:    "存在しないファイルはエラーを返す",
			write:   false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 対象のファイルパス
			path := filepath.Join(t.TempDir(), "00001-12345.png")
			if tt.write {
				if err := os.WriteFile(path, tt.content, 0o644); err != nil {
					t.Fatal(err)
				}
			}

			// When: パスを指定して読み込む
			got, err := ReadFile(path)

			// Then: parameters が取れるか、エラーになる
			if tt.wantErr {
				if err == nil {
					t.Fatal("error = nil, want non-nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			if got.Parameters != tt.want {
				t.Errorf("Parameters = %q", got.Parameters)
			}
		})
	}
}

package repository

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"testing"
)

func TestParseJSONIndex(t *testing.T) {
	t.Parallel()
	data := []byte(`[{"name":"Aniyomi: Example","pkg":"org.example.anime.demo","apk":"demo.apk","iconUrl":"demo.png","version":"1.2.3","nsfw":1,"sources":[{"id":"42","name":"Example","lang":"en"}]}]`)
	items, err := Parse(data, "https://repo.example/index.min.json", "anime")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %#v", items)
	}
	item := items[0]
	if item.Name != "Example" || item.APKURL != "https://repo.example/apk/demo.apk" || item.IconURL != "https://repo.example/icon/demo.png" || !item.NSFW {
		t.Fatalf("item = %#v", item)
	}
}

func TestParseGzippedIndex(t *testing.T) {
	t.Parallel()
	raw, _ := json.Marshal([]rawExtension{{
		Name: "Tachiyomi: Manga", Package: "org.example.manga.demo", APK: "manga.apk",
	}})
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	items, err := Parse(compressed.Bytes(), "https://repo.example/index.pb.gz", "manga")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Type != "manga" {
		t.Fatalf("items = %#v", items)
	}
}

func TestParseProtobufIndex(t *testing.T) {
	t.Parallel()
	resources := protoBytesField(1, []byte("https://repo.example/apk/demo.apk"))
	source := append(protoVarintField(1, 42), protoBytesField(3, []byte("en"))...)
	extension := []byte{}
	extension = append(extension, protoBytesField(1, []byte("Aniyomi: Example"))...)
	extension = append(extension, protoBytesField(2, []byte("org.example.anime.demo"))...)
	extension = append(extension, protoBytesField(3, resources)...)
	extension = append(extension, protoBytesField(6, []byte("1.0.0"))...)
	extension = append(extension, protoBytesField(8, source)...)
	root := protoBytesField(1, extension)
	items, err := Parse(root, "https://repo.example/index.pb", "anime")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Package != "org.example.anime.demo" || items[0].Language != "en" {
		t.Fatalf("items = %#v", items)
	}
}

func protoBytesField(field uint64, value []byte) []byte {
	result := encodeVarint(field<<3 | 2)
	result = append(result, encodeVarint(uint64(len(value)))...)
	return append(result, value...)
}

func protoVarintField(field, value uint64) []byte {
	result := encodeVarint(field << 3)
	return append(result, encodeVarint(value)...)
}

func encodeVarint(value uint64) []byte {
	var result []byte
	for value >= 0x80 {
		result = append(result, byte(value&0x7f)|0x80)
		value >>= 7
	}
	return append(result, byte(value))
}

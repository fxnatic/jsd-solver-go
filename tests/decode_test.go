package tests

import (
	"fmt"
	"os"
	"testing"

	"github.com/fxnatic/jsd-solver-go/utils"
	"github.com/fxnatic/jsd-solver-go/visitors"
	"github.com/t14raptor/go-fast/parser"
)

// TestDecodePayload tests decoding an LZ-String compressed payload using an extracted alphabet.
// Usage: go test ./tests -run TestDecodePayload -v
//
// To use:
// 1. Place your script in tests/script.js
// 2. Set the encoded payload in the test
func TestDecodePayload(t *testing.T) {
	// Read script from file
	scriptBytes, err := os.ReadFile("script.js")
	if err != nil {
		t.Fatalf("Failed to read script.js: %v\nMake sure tests/script.js exists", err)
	}

	prog, err := parser.ParseFile(string(scriptBytes))
	if err != nil {
		t.Fatalf("Failed to parse script: %v", err)
	}

	result, err := visitors.DeobfuscateCf(prog)
	if err != nil {
		t.Fatalf("Deobfuscation failed: %v", err)
	}

	fmt.Printf("Extracted LZ Alphabet: %s\n", result.LZAlphabet)

	encodedPayload := ""

	lz := utils.NewLZString(result.LZAlphabet)
	decoded := lz.DecompressFromBase64(encodedPayload)

	fmt.Printf("\nEncoded: %s\n", encodedPayload)
	fmt.Printf("Decoded: %s\n", decoded)
}

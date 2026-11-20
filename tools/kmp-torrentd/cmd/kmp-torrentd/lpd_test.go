package main

import (
	"strings"
	"testing"
)

func TestLocalDiscoveryMessageRoundTrip(t *testing.T) {
	hashes := []string{strings.Repeat("a", 40), strings.Repeat("b", 40)}
	packets := lpdMessages(6881, "c00k1e", hashes)
	if len(packets) != 1 {
		t.Fatalf("esperava um pacote, veio %d", len(packets))
	}
	port, parsed, cookie, ok := parseLPD(packets[0])
	if !ok || port != 6881 || cookie != "c00k1e" || len(parsed) != 2 || parsed[0] != hashes[0] || parsed[1] != hashes[1] {
		t.Fatalf("anúncio lido errado: %v %v %q %v", port, parsed, cookie, ok)
	}
}

func TestLocalDiscoverySplitsLargeAnnouncements(t *testing.T) {
	var hashes []string
	for i := 0; i < 100; i++ {
		hashes = append(hashes, strings.Repeat(string(rune('a'+i%6)), 40))
	}
	total := 0
	for _, packet := range lpdMessages(6881, "x", hashes) {
		if len(packet) > lpdMaxPacket {
			t.Fatalf("pacote com %d bytes passa do limite", len(packet))
		}
		_, parsed, _, ok := parseLPD(packet)
		if !ok {
			t.Fatal("pacote dividido não pôde ser lido")
		}
		total += len(parsed)
	}
	if total != 100 {
		t.Fatalf("esperava 100 infohashes, vieram %d", total)
	}
}

func TestLocalDiscoveryRejectsOtherMessages(t *testing.T) {
	for _, packet := range []string{
		"M-SEARCH * HTTP/1.1\r\nPort: 6881\r\nInfohash: " + strings.Repeat("a", 40) + "\r\n\r\n",
		"BT-SEARCH * HTTP/1.1\r\nPort: 99999\r\nInfohash: " + strings.Repeat("a", 40) + "\r\n\r\n",
		"BT-SEARCH * HTTP/1.1\r\nPort: 6881\r\nInfohash: " + strings.Repeat("a", 64) + "\r\n\r\n",
	} {
		if _, _, _, ok := parseLPD([]byte(packet)); ok {
			t.Fatalf("mensagem inválida aceita: %q", packet)
		}
	}
	// Cabeçalhos sem diferenciar maiúsculas, como alguns clientes enviam.
	port, hashes, _, ok := parseLPD([]byte("BT-SEARCH * HTTP/1.1\r\nhost: x\r\nPORT: 51413\r\ninfohash: " + strings.Repeat("F", 40) + "\r\n\r\n"))
	if !ok || port != 51413 || hashes[0] != strings.Repeat("f", 40) {
		t.Fatalf("anúncio de outro cliente lido errado: %v %v %v", port, hashes, ok)
	}
}

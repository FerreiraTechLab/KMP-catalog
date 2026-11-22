package main

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
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

// Quando o console troca de rede, serve devolve o motivo para os sockets serem reabertos
// e esquece os anúncios já feitos, para todos os torrents saírem de novo na rede nova.
func TestLPDServeReabreQuandoARedeMuda(t *testing.T) {
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	networks := []string{"192.168.0.98/24", "10.243.229.184/24"}
	calls := 0
	ld := &localDiscovery{d: &daemon{items: map[string]*transfer{}, byHash: map[string]string{}},
		port: 6881, cookie: "teste", wake: make(chan struct{}, 1),
		announced: map[string]time.Time{"aa": time.Now()}, seen: map[string]map[string]time.Time{},
		network: func() string {
			calls++
			return networks[1]
		}}
	ld.wake <- struct{}{}
	done := make(chan string, 1)
	go func() {
		done <- ld.serve(context.Background(), listener, sender, listener.LocalAddr().(*net.UDPAddr), networks[0])
	}()
	select {
	case reason := <-done:
		if !strings.Contains(reason, "rede local mudou") || !strings.Contains(reason, networks[1]) {
			t.Fatalf("motivo inesperado: %q", reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve não percebeu a troca de rede")
	}
	ld.mu.Lock()
	defer ld.mu.Unlock()
	if len(ld.announced) != 0 {
		t.Fatalf("os anúncios anteriores deviam ser esquecidos: %v", ld.announced)
	}
}

func TestLPDNetworkIgnoraLoopback(t *testing.T) {
	if strings.Contains(lpdNetwork(), "127.0.0.1") {
		t.Fatal("a assinatura da rede não deve incluir o loopback")
	}
}

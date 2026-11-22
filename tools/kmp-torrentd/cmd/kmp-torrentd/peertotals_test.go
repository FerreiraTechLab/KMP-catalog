package main

import "testing"

func TestPeerTotalsSomaConexoesEncerradas(t *testing.T) {
	pt := newPeerTotals()
	node := peerPath{IP: "163.176.18.211", Source: "M", Network: "tcp4"}
	lan := peerPath{IP: "192.168.0.57", Source: "Lpd", Network: "udp4"}
	pt.add("aa", node, 100, 5)
	pt.add("aa", node, 50, 0)
	pt.add("aa", lan, 300, 0)
	pt.add("bb", node, 999, 0)
	got := pt.report("aa", nil)
	if len(got) != 2 {
		t.Fatalf("esperava 2 caminhos, veio %d", len(got))
	}
	// Ordenado pelo que mais entregou.
	if got[0]["ip"] != "192.168.0.57" || got[0]["downloadedBytes"] != "300" {
		t.Fatalf("primeiro caminho errado: %v", got[0])
	}
	if got[1]["downloadedBytes"] != "150" || got[1]["uploadedBytes"] != "5" || got[1]["closedConnections"] != "2" {
		t.Fatalf("totais do nó errados: %v", got[1])
	}
	pt.forget("aa")
	if len(pt.report("aa", nil)) != 0 {
		t.Fatal("forget não limpou o torrent")
	}
}

func TestPeerTotalsLimite(t *testing.T) {
	pt := newPeerTotals()
	for i := 0; i < peerTotalsMax+10; i++ {
		pt.add("aa", peerPath{IP: string(rune('a' + i%26)), Source: "Hg", Network: string(rune(i))}, 1, 0)
	}
	if n := len(pt.closed["aa"]); n != peerTotalsMax {
		t.Fatalf("esperava o limite de %d caminhos, veio %d", peerTotalsMax, n)
	}
}

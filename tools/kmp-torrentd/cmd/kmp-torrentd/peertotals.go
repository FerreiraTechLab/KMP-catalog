package main

import (
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/anacrolix/torrent"
)

// Limite de caminhos guardados por torrent; um swarm comum de console fica muito abaixo disso.
const peerTotalsMax = 1024

// peerPath agrupa conexões pelo caminho por onde os dados passaram: endereço e porta remotos, origem
// do peer (tracker, DHT, PEX, LPD, nó do catálogo, hole punching, entrada) e transporte (tcp4, udp6…).
// A porta separa aparelhos atrás do mesmo IPv4 público, como um PC e um console da mesma casa.
type peerPath struct {
	IP, Port, Source, Network string
}

type peerTotal struct {
	Down, Up int64
	Closed   int
}

// peerTotals soma os bytes das conexões já encerradas. Sem isso, o que uma conexão trocou some
// do aria2.getPeers assim que ela fecha, e o diagnóstico perde a origem desses dados.
type peerTotals struct {
	mu     sync.Mutex
	closed map[string]map[peerPath]*peerTotal
}

func newPeerTotals() *peerTotals {
	return &peerTotals{closed: map[string]map[peerPath]*peerTotal{}}
}

func pathOf(p *torrent.PeerConn) peerPath {
	host, port := "", ""
	if p.RemoteAddr != nil {
		host, port, _ = net.SplitHostPort(p.RemoteAddr.String())
	}
	return peerPath{IP: host, Port: port, Source: string(p.Discovery), Network: p.Network}
}

func (pt *peerTotals) add(hash string, path peerPath, down, up int64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	paths := pt.closed[hash]
	if paths == nil {
		paths = map[peerPath]*peerTotal{}
		pt.closed[hash] = paths
	}
	total := paths[path]
	if total == nil {
		if len(paths) >= peerTotalsMax {
			return
		}
		total = &peerTotal{}
		paths[path] = total
	}
	total.Down += down
	total.Up += up
	total.Closed++
}

// connClosed é o callback PeerConnClosed da anacrolix. A biblioteca o chama com a trava do cliente
// tomada, e Peer.Stats() tenta tomá-la de novo: ler os contadores ali trava o daemon inteiro. Por isso
// a leitura vai para uma goroutine, que roda depois que a trava é liberada; a conexão encerrada
// continua em memória com os contadores finais.
func (pt *peerTotals) connClosed(p *torrent.PeerConn) {
	go func() {
		t := p.Torrent()
		if t == nil {
			return
		}
		stats := p.Stats()
		pt.add(strings.ToLower(t.InfoHash().HexString()), pathOf(p), stats.BytesReadUsefulData.Int64(),
			stats.BytesWrittenData.Int64())
	}()
}

func (pt *peerTotals) forget(hash string) {
	pt.mu.Lock()
	delete(pt.closed, hash)
	pt.mu.Unlock()
}

// report junta os totais das conexões encerradas com as abertas, por caminho.
func (pt *peerTotals) report(hash string, open []*torrent.PeerConn) []map[string]any {
	merged := map[peerPath]*peerTotal{}
	pt.mu.Lock()
	for path, total := range pt.closed[hash] {
		copied := *total
		merged[path] = &copied
	}
	pt.mu.Unlock()
	openCount := map[peerPath]int{}
	for _, p := range open {
		path := pathOf(p)
		stats := p.Stats()
		total := merged[path]
		if total == nil {
			total = &peerTotal{}
			merged[path] = total
		}
		total.Down += stats.BytesReadUsefulData.Int64()
		total.Up += stats.BytesWrittenData.Int64()
		openCount[path]++
	}
	paths := make([]peerPath, 0, len(merged))
	for path := range merged {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool { return merged[paths[i]].Down > merged[paths[j]].Down })
	ret := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		total := merged[path]
		ret = append(ret, map[string]any{"ip": path.IP, "port": path.Port, "source": path.Source, "network": path.Network,
			"downloadedBytes": strconv.FormatInt(total.Down, 10), "uploadedBytes": strconv.FormatInt(total.Up, 10),
			"closedConnections": strconv.Itoa(total.Closed), "openConnections": strconv.Itoa(openCount[path])})
	}
	return ret
}

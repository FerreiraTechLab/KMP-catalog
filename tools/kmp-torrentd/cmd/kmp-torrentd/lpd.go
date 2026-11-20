package main

// Descoberta local de peers (BEP 14). Dois consoles atrás do mesmo roteador só se viam
// pelo IP público da rede, que o roteador doméstico não roteia de volta para dentro
// (hairpin NAT); tracker, DHT, PEX e o nó público continuam valendo para peers de
// outras redes, e esta descoberta apenas acrescenta os peers da rede local.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
)

const (
	lpdGroup = "239.192.152.143:6771"
	// O BEP 14 pede no máximo um anúncio por torrent a cada minuto. Um torrent ainda
	// baixando usa esse mínimo; um completo, cinco minutos. Um torrent novo é anunciado
	// na hora, e os anúncios de torrents que ainda não temos ficam guardados: quando o
	// download começa, os peers da rede local já entram, mesmo que o download dure menos
	// que o intervalo dos outros consoles.
	lpdIntervalComplete   = 5 * time.Minute
	lpdIntervalIncomplete = time.Minute
	lpdTick               = 15 * time.Second
	lpdRemember           = 10 * time.Minute
	lpdRememberMax        = 4096
	lpdMaxPacket          = 1400

	peerSourceLocal torrent.PeerSource = "Lpd"
)

type localDiscovery struct {
	d         *daemon
	port      int
	cookie    string
	wake      chan struct{}
	mu        sync.Mutex
	announced map[string]time.Time
	seen      map[string]map[string]time.Time
}

// lpdMessages monta os anúncios, vários infohashes por pacote sem passar de 1400 bytes.
func lpdMessages(port int, cookie string, hashes []string) [][]byte {
	head := fmt.Sprintf("BT-SEARCH * HTTP/1.1\r\nHost: %s\r\nPort: %d\r\n", lpdGroup, port)
	tail := fmt.Sprintf("cookie: %s\r\n\r\n\r\n", cookie)
	var packets [][]byte
	var body strings.Builder
	flush := func() {
		if body.Len() > 0 {
			packets = append(packets, []byte(head+body.String()+tail))
			body.Reset()
		}
	}
	for _, hash := range hashes {
		line := "Infohash: " + hash + "\r\n"
		if len(head)+body.Len()+len(line)+len(tail) > lpdMaxPacket {
			flush()
		}
		body.WriteString(line)
	}
	flush()
	return packets
}

// parseLPD lê um anúncio BEP 14. Só infohashes v1 (40 hex) interessam ao daemon.
func parseLPD(packet []byte) (port int, hashes []string, cookie string, ok bool) {
	scanner := bufio.NewScanner(bytes.NewReader(packet))
	if !scanner.Scan() || !strings.HasPrefix(strings.TrimSpace(scanner.Text()), "BT-SEARCH * HTTP/1.1") {
		return 0, nil, "", false
	}
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), ":")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "port":
			port, _ = strconv.Atoi(value)
		case "infohash":
			if hash := strings.ToLower(value); len(hash) == 40 && isHex(hash) {
				hashes = append(hashes, hash)
			}
		case "cookie":
			cookie = value
		}
	}
	return port, hashes, cookie, port > 0 && port <= 65535 && len(hashes) > 0
}

func isHex(value string) bool {
	_, err := hex.DecodeString(value)
	return err == nil
}

func (d *daemon) runLocalDiscovery(ctx context.Context, port int) {
	group, err := net.ResolveUDPAddr("udp4", lpdGroup)
	if err != nil {
		log.Printf("descoberta local indisponível: %v", err)
		return
	}
	listener, err := net.ListenMulticastUDP("udp4", nil, group)
	if err != nil {
		log.Printf("descoberta local indisponível: %v", err)
		return
	}
	sender, err := net.DialUDP("udp4", nil, group)
	if err != nil {
		listener.Close()
		log.Printf("descoberta local indisponível: %v", err)
		return
	}
	token := make([]byte, 8)
	_, _ = rand.Read(token)
	ld := &localDiscovery{d: d, port: port, cookie: hex.EncodeToString(token), wake: make(chan struct{}, 1),
		announced: map[string]time.Time{}, seen: map[string]map[string]time.Time{}}
	d.lpd.Store(ld)
	go func() {
		<-ctx.Done()
		listener.Close()
		sender.Close()
	}()
	go ld.receive(listener)
	ticker := time.NewTicker(lpdTick)
	defer ticker.Stop()
	for {
		ld.announce(sender, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-ld.wake:
		}
	}
}

// announce envia os torrents ativos cujo intervalo venceu. Torrents pausados (por
// bateria, por exemplo) ficam de fora, porque não enviam dados.
func (ld *localDiscovery) announce(sender *net.UDPConn, now time.Time) {
	ld.d.mu.RLock()
	var due []string
	ld.mu.Lock()
	for _, x := range ld.d.items {
		hash := strings.ToLower(x.Torrent.InfoHash().HexString())
		interval := lpdIntervalComplete
		if x.Torrent.Info() == nil || x.Torrent.BytesMissing() > 0 {
			interval = lpdIntervalIncomplete
		}
		if !x.Paused && now.Sub(ld.announced[hash]) >= interval {
			due = append(due, hash)
			ld.announced[hash] = now
		}
	}
	ld.mu.Unlock()
	ld.d.mu.RUnlock()
	for _, packet := range lpdMessages(ld.port, ld.cookie, due) {
		if _, err := sender.Write(packet); err != nil {
			log.Printf("descoberta local: falha ao anunciar: %v", err)
			return
		}
	}
}

func (ld *localDiscovery) receive(listener *net.UDPConn) {
	buffer := make([]byte, 2048)
	for {
		n, source, err := listener.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		port, hashes, cookie, ok := parseLPD(buffer[:n])
		if !ok || cookie == ld.cookie {
			continue
		}
		addr := net.JoinHostPort(source.IP.String(), strconv.Itoa(port))
		now := time.Now()
		for _, hash := range hashes {
			ld.d.mu.RLock()
			x := ld.d.items[ld.d.byHash[hash]]
			ld.d.mu.RUnlock()
			if x != nil {
				x.Torrent.AddPeers([]torrent.PeerInfo{{Addr: torrent.StringAddr(addr), Source: peerSourceLocal}})
			} else {
				ld.remember(hash, addr, now)
			}
		}
	}
}

// remember guarda o anúncio de um torrent que ainda não temos, com limite de tamanho.
func (ld *localDiscovery) remember(hash, addr string, now time.Time) {
	ld.mu.Lock()
	defer ld.mu.Unlock()
	if ld.seen[hash] == nil {
		if len(ld.seen) >= lpdRememberMax {
			for key, peers := range ld.seen {
				for peer, at := range peers {
					if now.Sub(at) > lpdRemember {
						delete(peers, peer)
					}
				}
				if len(peers) == 0 {
					delete(ld.seen, key)
				}
			}
			if len(ld.seen) >= lpdRememberMax {
				return
			}
		}
		ld.seen[hash] = map[string]time.Time{}
	}
	ld.seen[hash][addr] = now
}

// added entrega a um torrent recém-adicionado os peers da rede local já conhecidos e
// pede um anúncio imediato dele.
func (ld *localDiscovery) added(hash string, x *transfer) {
	now := time.Now()
	var peers []torrent.PeerInfo
	ld.mu.Lock()
	for addr, at := range ld.seen[hash] {
		if now.Sub(at) <= lpdRemember {
			peers = append(peers, torrent.PeerInfo{Addr: torrent.StringAddr(addr), Source: peerSourceLocal})
		}
	}
	delete(ld.seen, hash)
	delete(ld.announced, hash)
	ld.mu.Unlock()
	if len(peers) > 0 {
		x.Torrent.AddPeers(peers)
	}
	select {
	case ld.wake <- struct{}{}:
	default:
	}
}

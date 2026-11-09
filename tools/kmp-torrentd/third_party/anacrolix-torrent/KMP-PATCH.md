# anacrolix/torrent v1.61.0 com um remendo

Cópia da v1.61.0 (sem testes, `cmd/` e `fs/`) usada pelo `kmp-torrentd` via `replace` no `go.mod`.

Única alteração: `webseed-requesting.go`, em `iterCurrentWebseedRequestsFromClient`, ignora pedidos
de torrents já removidos. É o commit upstream 4bd78e745c ("Don't panic when a dropped torrent still
has webseed requests in flight", #1112), que ainda não saiu em versão. Sem ele, remover um torrent
com pedidos em andamento a um web seed derruba o processo com `panic: false`.

Remova esta pasta e o `replace` quando sair uma versão com essa correção.

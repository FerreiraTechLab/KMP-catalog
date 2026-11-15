# anacrolix/torrent v1.61.0 com dois remendos

Cópia da v1.61.0 (sem testes, `cmd/` e `fs/`) usada pelo `kmp-torrentd` via `replace` no `go.mod`.

Ambos são commits upstream que ainda não saíram em versão:

1. `webseed-requesting.go`, em `iterCurrentWebseedRequestsFromClient`, ignora pedidos de torrents já
   removidos. É o 4bd78e745c ("Don't panic when a dropped torrent still has webseed requests in
   flight", #1112). Sem ele, remover um torrent com pedidos em andamento a um web seed derruba o
   processo com `panic: false`.
2. `storage/file-client.go` ganha `NewFileClientOpts.DisableMmap`. É o 6eade025b5 ("storage: let a
   file client ask for the classic file IO", #1115), adaptado para o `classicFileIo{}` da v1.61. O
   padrão mapeia todos os arquivos em memória e nunca os libera: num console com 1 GB de RAM que
   semeia vários GB, as páginas lidas se acumulam no processo, e um arquivo trocado por fora derruba
   o daemon com SIGBUS.

Remova esta pasta e o `replace` quando sair uma versão com essas correções.

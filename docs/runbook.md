# gowa — Runbook

## Como rodar

Comandos Go sempre a partir de `src/` — **a raiz do repo não é o módulo Go**.

```
cd src && go run . rest      # API REST, porta padrão 3000
cd src && go run . mcp       # servidor MCP (SSE)
cd src && go build -o whatsapp
cd src && go test ./...
cd src && go vet ./...
docker compose up --build    # compose monta ./storages e ./statics da raiz
```

Debug: `--debug=true` ou `APP_DEBUG=true` (logs `Chatwoot:` e `Chatwoot Webhook:`).

## Deploy / Produção

Produção: imagens Docker locais taggeadas manualmente (ex. `gowa-pr739:quote-fix3`) rodando
as instâncias `zap*`. Procedimento exato de deploy (host/orquestrador/comando) ainda não está
documentado — anotar aqui na próxima operação.

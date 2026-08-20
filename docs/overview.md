# gowa — Visão geral

Fork local do **go-whatsapp-web-multidevice** (aldinokemal) — servidor de API WhatsApp Web
em Go, com modos REST e MCP (SSE). Usado como ponte WhatsApp ↔ **Chatwoot** para atendimento
(instâncias `zap9273`, `zap8586`, `zap5827`, `zapvig`). O fork carrega melhorias próprias
(Chatwoot quote/threading, filename de mídia, etc.) por cima do upstream.

Antes de mexer no código, leia as invariantes em [AGENTS.md](../AGENTS.md).

## Stack

- Go 1.25 — Fiber (REST), whatsmeow (protocolo WhatsApp), Cobra/Viper (CLI/config),
  ozzo-validation.
- SQLite como chat storage — `mattn/go-sqlite3` (CGO) por padrão; build com `-tags purego`
  usa `modernc.org/sqlite`.
- UI embutida em Vue 3 via CDN (sem build step), servida via `go:embed`.
- Docker Alpine multi-arch (`docker/golang.Dockerfile`).

## Repositório

`~/Desenvolvimento/gowa` — `origin` aponta para o **upstream**
(`aldinokemal/go-whatsapp-web-multidevice`). Branch de trabalho: `main`, à frente do upstream
com commits próprios (contribuições pontuais já foram aceitas upstream por PR, ex.
#714/#715). Regra de push: ver invariante em [AGENTS.md](../AGENTS.md).

## Arquitetura (resumo)

Camadas: `cmd/` (Cobra, wiring global) → `config/` (globals mutáveis de flags/env) →
`domains/` (DTOs e interfaces, sem lógica) → `usecase/` (valida → resolve device do contexto
→ chama whatsmeow/storage) → `ui/` (rest, mcp, websocket) → `infrastructure/` (`whatsapp/`
device manager + eventos, `chatstorage/` repositório SQL, `chatwoot/` sync REST + `pgimport/`
import direto no Postgres).

Pontos-chave:

- **Multi-device**: rotas REST device-scoped exigem `X-Device-Id` ou `device_id`; MCP resolve
  o device default via `ContextWithDefaultDevice`. REST e MCP não podem rodar juntos no mesmo
  processo/estado whatsmeow.
- Config: prioridade flags Cobra > env/Viper > `.env` carregado de `src/`.
- Sem DI: globals mutáveis de pacote para config, clients, repositórios e usecases.
- `AppVersion` hard-coded em `src/config/settings.go` (workflows não injetam via ldflags).
- CI: workflows do GitHub são só de release/publish — **não há gate de PR** rodando
  testes/vet/lint (ver pendência Wave 2 #005 em
  [`handoff/ESTADO_ATUAL.md`](../handoff/ESTADO_ATUAL.md)).

Convenções, anti-padrões e mapa "onde mexer para cada tarefa" completo: ver
[AGENTS.md](../AGENTS.md).

## Integração Chatwoot e Webhooks

Documentação técnica completa (modelo mental, env vars, payloads por evento, tabela de
erros, troubleshooting):

- [`chatwoot.md`](chatwoot.md) — integração Chatwoot (bidirecional, import de histórico,
  configuração, erros comuns).
- [`webhook-payload.md`](webhook-payload.md) — envelope, assinatura HMAC, retry, eventos e
  payloads.
- [`per-device-webhook-plan.md`](per-device-webhook-plan.md) — design do webhook por device
  (já implementado, upstream #671); mantido como referência de fluxo.
- [`openapi.yaml`](openapi.yaml) / [`sdk/config.yaml`](sdk/config.yaml) — specs técnicas da
  API REST.

Resumo do modelo mental: WhatsApp → Chatwoot ao vivo via REST (falha transitória vai para
fila de retry local; 4xx permanente não re-tenta); Chatwoot → WhatsApp via `POST
/chatwoot/webhook` (rota pública, protegida por `CHATWOOT_WEBHOOK_SECRET`); histórico via
REST ou, se `CHATWOOT_IMPORT_DB_URI` setado, direto no Postgres do Chatwoot (import é só
para histórico; ver invariante em [AGENTS.md](../AGENTS.md)).

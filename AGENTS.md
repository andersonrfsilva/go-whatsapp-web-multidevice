# gowa — invariantes de agente

Documentação de projeto (visão geral, stack, arquitetura, decisões, estado atual, docs
técnicas) vive em [CLAUDE.md](CLAUDE.md) e em `docs/`. Este arquivo cobre só o que um agente
precisa saber **antes de editar código**: onde mexer para cada tarefa e as regras que não dá
para deduzir olhando um arquivo isolado.

## Onde mexer (tarefas comuns)

| Tarefa | Onde |
|---|---|
| Novo tipo de mensagem | `src/domains/send/`, `src/usecase/send.go`, `src/ui/rest/send.go` (REST é o caminho primário; MCP é subconjunto) |
| Novo endpoint REST | `ui/rest/` (parse) → `usecase/` (valida/executa) → `domains/` (DTO/interface) |
| Nova tool MCP | `src/ui/mcp/`, registrar em `Add*Tools`, device via `helpers.ContextWithDefaultDevice` |
| Evento WhatsApp | `src/infrastructure/whatsapp/event_*.go`, registrar em `event_handler.go` |
| Método de chat storage | atualizar juntos: `domains/chatstorage/interfaces.go` + `sqlite_repository.go` + `chatstorage_wrapper.go` |
| Migração de DB | `sqlite_repository.go > getMigrations()` — **somente append** |
| Componente de UI | `src/views/components/` — módulos JS puros, sem `.vue`; delimitadores `[[ ]]` |
| Chatwoot | `src/infrastructure/chatwoot/` + `src/ui/rest/chatwoot.go`; import direto: `chatwoot/pgimport/` |

## Invariantes de segurança (INEGOCIÁVEIS)

- **NUNCA pushar para `origin`** — o remote aponta para o upstream de terceiros
  (`aldinokemal/go-whatsapp-web-multidevice`); nosso trabalho vive só no `main` local.
- **Migrações de DB são append-only** em `sqlite_repository.go > getMigrations()` —
  inserir no meio corrompe bancos existentes em produção.
- **Escopo de `device_id` obrigatório** em qualquer consulta de chats/mensagens em fluxo
  device-scoped; JIDs `@lid` sempre normalizados via `NormalizeJIDFromLID()`.
- **Não remover** o check `evt.Sender.Device != 0` de receipts (evita webhooks duplicados de
  devices linkados).
- **Import direto no Postgres do Chatwoot é SÓ para histórico** — forwarding ao vivo é
  sempre REST.
- **Credenciais nunca em arquivo versionado** — valores só no `.env` de cada instância (fora
  do git); nos docs, apenas os nomes (`CHATWOOT_API_TOKEN`, `CHATWOOT_ACCOUNT_ID`,
  `CHATWOOT_INBOX_ID`, `CHATWOOT_DEVICE_ID`, `CHATWOOT_WEBHOOK_SECRET`,
  `CHATWOOT_IMPORT_DB_URI`, `WHATSAPP_WEBHOOK_SECRET`, `APP_BASIC_AUTH`, ...).
- **Não commitar** mídia de runtime, QR codes, DBs SQLite (`storages/`, `statics/`) ou dumps
  de histórico.
- Paths de runtime (`storages/`, `statics/`) são relativos ao cwd do processo — comandos Go
  rodam a partir de `src/` (ver "Como rodar" em `CLAUDE.md`).

## Regras que salvam horas

- Usecase: valida primeiro → pega device/client do contexto → chama whatsmeow/storage.
- Rotas REST device-scoped devem propagar
  `whatsapp.ContextWithDevice(c.UserContext(), getDeviceFromCtx(c))`.
- Filtros booleanos opcionais usam `*bool` (nil = não informado).
- Testes como `*_test.go`, table-driven com `testify`; testes que mutam config/globals/workers
  ficam seriais e restauram estado com `defer`.
- `src/pkg/error` declara package `error` — importar com alias (`pkgError`).
- Depois de mudar `.env`, reiniciar o processo — rotas Chatwoot são registradas no startup.

## Anti-padrões (NUNCA)

- Consultar chats/mensagens sem escopo de `device_id` em fluxos device-scoped.
- Usar `instance.ID()` como `device_id` das tabelas após login — usar
  `client.Store.ID.ToNonAD().String()`.
- Usar JID `@lid` cru em lookup de DB — normalizar antes com `NormalizeJIDFromLID()`.
- Inserir migração no meio de `getMigrations()` — só append.
- Remover o check `evt.Sender.Device != 0` de receipts.
- Tratar o import direto no Postgres como caminho de forwarding ao vivo (é só para
  histórico).
- Commitar mídia de runtime, QR codes, DBs SQLite, `.env` ou dumps de histórico.

## Símbolos centrais

`DeviceManager` (registry de devices) · `DeviceInstance` (device + client + storage) ·
`SQLiteRepository` (storage + links/retries Chatwoot + migrações inline) ·
`StartChatwootForwardRetryWorker` (replay de forwards falhos) ·
`StartPresencePulseScheduler` (pulso diário de presença) ·
`NormalizeJIDFromLID` (primitivo crítico de roteamento) ·
`pgimport.Importer` (import histórico direto no PG).

## Antes de trabalhar

Leia `CLAUDE.md` (estado atual e pendências, datado) — em particular a mudança **não
commitada** em `src/infrastructure/whatsapp/webhook_forward.go`, que deve ser inspecionada
antes de qualquer novo trabalho na área de Chatwoot/reações.

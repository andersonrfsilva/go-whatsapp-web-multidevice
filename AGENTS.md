# gowa — a documentação vive no Atlas

Toda a documentação deste projeto (visão geral, decisões, estado, runbook, convenções,
Chatwoot, webhooks) está no **Atlas** (TriliumNext): `Atlas > Projetos > gowa` — acesso
via MCP `trilium`, helper `atlas` (ETAPI) ou o app Trilium. Este arquivo é apenas um
ponteiro com as invariantes de segurança, para humanos ou ferramentas sem acesso ao Atlas.

## Invariantes de segurança (INEGOCIÁVEIS)

- **NUNCA pushar para `origin`** — o remote aponta para o upstream de terceiros
  (`aldinokemal/go-whatsapp-web-multidevice`); nosso trabalho vive só no `main` local.
- **Migrações de DB são append-only** em `sqlite_repository.go > getMigrations()` —
  inserir no meio corrompe bancos existentes em produção.
- **Escopo de `device_id` obrigatório** em qualquer consulta de chats/mensagens em
  fluxo device-scoped; JIDs `@lid` sempre normalizados via `NormalizeJIDFromLID()`.
- **Não remover** o check `evt.Sender.Device != 0` de receipts (evita webhooks duplicados).
- **Import direto no Postgres do Chatwoot é SÓ para histórico** — forwarding ao vivo é REST.
- **Credenciais nunca em arquivo versionado** — valores só no `.env` (fora do git); nos
  docs, apenas os nomes (`CHATWOOT_API_TOKEN`, `WHATSAPP_WEBHOOK_SECRET`, `APP_BASIC_AUTH`, ...).
- **Não commitar** mídia de runtime, QR codes, DBs SQLite (`storages/`, `statics/`) ou dumps.
- Comandos Go sempre de `src/` (a raiz do repo não é o módulo Go).

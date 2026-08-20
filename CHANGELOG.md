# Changelog

## 2026-07-18 — Fix do quote Chatwoot→WhatsApp via paginação backward
- Replies de agente no Chatwoot que citavam mensagens do cliente passaram a chegar
  corretamente como quote no WhatsApp. A resolução do `source_id` da mensagem citada agora
  pagina o endpoint de mensagens do Chatwoot para trás (não só a primeira página) e decodifica
  `message_type` numérico corretamente. Corrige dois bugs de produção: decode numérico do tipo
  de mensagem e lookup limitado à primeira página (este último era o bloqueador real em
  conversas longas). Ver ADR-004.

## 2026-07-06 — Reconciliar branch de trabalho `pr739` com `main`
- Branch de trabalho `pr739` (integração Chatwoot) estava 15 commits atrás do `main` local,
  que já trazia webhooks por device e fixes vindos do upstream. Reconciliado via `git merge
  main` (sem conflitos), preservando o histórico dos dois lados e evitando reimplementar o
  que o `main` já resolvia. Ver ADR-003.

## (sem data) — Import direto no Postgres do Chatwoot para histórico
- Quando `CHATWOOT_IMPORT_DB_URI` está configurado, o sync de histórico passou a escrever
  direto nas tabelas Postgres do Chatwoot (idempotente por `inbox_id + source_id`), evitando o
  timestamp incorreto imposto pela API REST do Chatwoot (que não permite definir `created_at`).
  O forwarding ao vivo continua sempre via REST, para preservar o pipeline normal de eventos do
  Chatwoot (automações, sockets, atribuição). Ver ADR-002.

## (sem data) — Fork local sem push para o upstream
- Definido que o trabalho próprio segue em `main` local, à frente do upstream
  (`aldinokemal/go-whatsapp-web-multidevice`), sem push para `origin`. Contribuições pontuais
  continuam sendo enviadas ao upstream via PR quando fizer sentido (ex. #714/#715). Decisão de
  base que sustenta a liberdade para mudanças específicas da integração Chatwoot sem depender
  do ciclo de release do upstream. Ver ADR-001.

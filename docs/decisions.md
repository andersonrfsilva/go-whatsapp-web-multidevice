# gowa — Decisões técnicas (mais recentes primeiro)

Registro estilo ADR informal. Estado atual e pendências ficam em `handoff/`, não aqui.

### ADR-004 — 2026-07-18 — Fix do quote Chatwoot→WhatsApp via paginação backward (plano 007)

**Contexto:** replies de agente no Chatwoot citando mensagens do cliente não chegavam como
quote no WhatsApp. **Decisão:** resolver o `source_id` da mensagem citada paginando o
endpoint de mensagens do Chatwoot para trás (não só a primeira página) e decodificando
`message_type` numérico. **Porquê:** dois bugs achados em produção — decode numérico
(`3dde024`) e lookup limitado à primeira página, o bloqueador real (`69ac2e8`). Validado pelo
usuário em 2026-07-18 ("agora foi!"). **Alternativas descartadas:** manter lookup de página
única (falhava em conversas longas).

### ADR-003 — 2026-07-06 — Reconciliar `pr739` com `main` por merge (não rebase)

**Contexto:** branch de trabalho `pr739` estava 15 commits atrás do `main` local (que trazia
per-device webhooks e fixes do upstream); executores de planos criavam worktrees a partir de
`main`, causando divergência. **Decisão:** `git merge main` em `pr739` (auto-merge sem
conflitos), com branch de segurança `backup/pr739-pre-reconcile`. **Porquê:** evitar
reimplementar o que o `main` já tinha (ex. plano 003 já resolvido por `23620dc`); manter
histórico dos dois lados. **Alternativas descartadas:** rebase (reescreveria histórico local
compartilhado com worktrees).

### ADR-002 — Import direto no Postgres do Chatwoot só para histórico (sem data registrada)

**Contexto:** a API REST do Chatwoot não permite definir `created_at`, então histórico
importado ficava com timestamp errado. **Decisão:** quando `CHATWOOT_IMPORT_DB_URI` está
setado, o sync histórico escreve direto nas tabelas Postgres do Chatwoot (idempotente por
`inbox_id + source_id`); o forwarding **ao vivo continua via REST** para disparar o pipeline
normal de eventos do Chatwoot (automações, sockets, atribuição). **Porquê:** preserva
timestamps/nomes de grupo originais e é muito mais rápido; REST ao vivo mantém o
comportamento do produto. **Alternativas descartadas:** tudo via REST (timestamps errados)
ou tudo via DB (quebraria automações/eventos do Chatwoot).

### ADR-001 — Fork local sem push para o upstream (contínua)

**Contexto:** `origin` é o repositório do autor original (aldinokemal). **Decisão:** manter o
trabalho próprio em `main` local, à frente do upstream, sem push; contribuições pontuais já
foram aceitas upstream por PR (ex. #714/#715 na era do task-loop). **Porquê:** liberdade para
mudanças específicas do nosso uso (Chatwoot) sem depender do ciclo do upstream. Risco
assumido: conflitos a cada pull do upstream — a camada Chatwoot toca 10+ arquivos do upstream
(ver spike de design "ChatwootSink" em `plans/README.md`, Wave 3).

# 007 — Fix: citação (reply) do Chatwoot não chega ao WhatsApp para mensagens do cliente

## Status — ✅ RESOLVIDO E EM PRODUÇÃO (2026-07-18)
- **Resolvido e validado pelo usuário** no Chatwoot do `zap9273` ("agora foi!").
- **Em produção:** imagem `gowa-pr739:quote-fix3` (commit `69ac2e8` em `main`), **apenas no `zap9273`** por decisão do usuário (zap8586/zap5827/zapvig seguem em `reply-threading`; a tag nova isola o deploy).
- **Decisões do usuário:** commitar direto no `main`; deploy só no `zap9273`.
- Ver a seção **"Resolução final"** no fim deste arquivo — o fix real foi diferente (e mais simples) do desenhado originalmente abaixo. O corpo original é mantido como registro do diagnóstico inicial.

## Sintoma
Quando um agente responde no Chatwoot usando "reply" (citando uma mensagem), a mensagem chega ao cliente no WhatsApp **sem o balãozinho de citação** (texto puro).

## Causa raiz (confirmada)
A resolução da citação Chatwoot→WhatsApp (`resolveReplyMessageID` em `src/ui/rest/chatwoot.go`) depende **exclusivamente** da tabela local `chatwoot_message_links`. Em produção (`zap9273`):

- 1417 links, **100% `direction='outgoing'`**.
- **0 links `incoming`**, apesar de 17.344 mensagens de clientes (`is_from_me=0`) armazenadas.
- JOIN confirmou: `incoming msgs com link = 0`; `outgoing com link = 1417`.

Como o agente normalmente cita uma **mensagem do cliente (incoming)**, a etapa 1 não acha link, retorna `nil` (fallback silencioso, log em nível Debug) e a mensagem sai sem quote. A citação só funciona hoje quando o agente cita uma mensagem que a **própria conta** enviou (essas têm link `outgoing`).

Observação: não ficou 100% esclarecido *por que* mensagens incoming não geram link (o forwarder em `webhook_forward.go:1200-1206` deveria gravar para todo evento "message", independente de direção). O fix abaixo **contorna** isso de forma robusta sem depender de descobrir o motivo nem de backfill.

## Fix — fallback via `source_id` da API do Chatwoot
O forwarder carimba **toda** mensagem espelhada (inclusive do cliente) com `source_id = "WAID:<id-whatsapp>"` no Chatwoot. Então o ID do WhatsApp da mensagem citada existe no Chatwoot mesmo sem link local — basta buscá-lo.

### Alteração 1 — `src/infrastructure/chatwoot/types.go`
No struct `Message`, adicionar após `ContentType`:
```go
	// SourceID carries the external id the forwarder stamps on every mirrored
	// WhatsApp message ("WAID:<whatsapp-message-id>"). It lets an agent reply
	// resolve the quoted message back to its WhatsApp id even when no local
	// link row exists (notably inbound contact messages).
	SourceID string `json:"source_id"`
```

### Alteração 2 — `src/infrastructure/chatwoot/client.go`
Novo método (segue o padrão de `FindContactByIdentifier`: `http.NewRequest("GET")`, header `api_access_token`, `HTTPStatusError`):
```go
// GetMessageSourceID lista as mensagens da conversa e devolve o source_id da
// mensagem com o id dado, ou "" se não achar.
func (c *Client) GetMessageSourceID(conversationID, messageID int) (string, error) {
	if conversationID == 0 || messageID == 0 {
		return "", nil
	}
	endpoint := fmt.Sprintf("%s/api/v1/accounts/%d/conversations/%d/messages", c.BaseURL, c.AccountID, conversationID)
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("api_access_token", c.APIToken)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", &HTTPStatusError{StatusCode: resp.StatusCode, Op: "list messages", Body: string(body)}
	}
	var result struct {
		Payload []Message `json:"payload"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	for _, m := range result.Payload {
		if m.ID == messageID {
			return m.SourceID, nil
		}
	}
	return "", nil
}
```
> Nota: o endpoint `GET /api/v1/accounts/{id}/conversations/{cid}/messages` do Chatwoot pode paginar. Para conversas longas, a mensagem citada pode não estar na primeira página — validar em teste real; se necessário, aceitar paginação (`?before=`) ou usar o endpoint de mensagem individual, se disponível na versão do Chatwoot em uso.

### Alteração 3 — `src/ui/rest/chatwoot.go`
Adicionar seam testável e reescrever `resolveReplyMessageID` para cair no fallback quando o link local falhar (`strings` já está importado):
```go
// seam para teste
var resolveQuotedSourceIDFn = func(conversationID, messageID int) (string, error) {
	cw := chatwoot.GetDefaultClient()
	if cw == nil || !cw.IsConfigured() {
		return "", nil
	}
	return cw.GetMessageSourceID(conversationID, messageID)
}

func (h *ChatwootHandler) resolveReplyMessageID(deviceID string, payload chatwoot.WebhookPayload) *string {
	chatwootID := chatwootInReplyToID(payload.ContentAttributes)
	if chatwootID == 0 {
		return nil
	}
	// 1) Link local (rápido; cobre mensagens com link já armazenado, ex.: outgoing).
	if h.ChatStorageRepo != nil && deviceID != "" {
		link, err := h.ChatStorageRepo.GetChatwootMessageLinkByChatwootID(deviceID, chatwootID)
		if err != nil {
			logrus.Warnf("Chatwoot Webhook: Failed to resolve quoted Chatwoot message %d: %v", chatwootID, err)
		} else if link != nil && link.WhatsAppMessageID != "" {
			waID := link.WhatsAppMessageID
			return &waID
		}
	}
	// 2) Fallback: source_id ("WAID:<id>") da mensagem citada via API do Chatwoot —
	// cobre mensagens do cliente (incoming) que não têm link local.
	sourceID, err := resolveQuotedSourceIDFn(payload.Conversation.ID, chatwootID)
	if err != nil {
		logrus.Warnf("Chatwoot Webhook: Failed to fetch source_id for quoted message %d: %v", chatwootID, err)
	} else if waID := strings.TrimPrefix(sourceID, "WAID:"); sourceID != "" && waID != sourceID && waID != "" {
		return &waID
	}
	logrus.Debugf("Chatwoot Webhook: No WhatsApp id for quoted Chatwoot message %d, sending without quote", chatwootID)
	return nil
}
```

### Teste (chatwoot_test.go)
- Sobrescrever `resolveQuotedSourceIDFn` para retornar `"WAID:ABC123"` e verificar que `resolveReplyMessageID` devolve `"ABC123"` quando não há link local.
- Verificar que `source_id` sem prefixo `WAID:` (ou vazio) resulta em `nil`.
- Manter o teste existente do caminho por link local passando.

## Verificação local
```
cd ~/Desenvolvimento/gowa/src
go build ./...
go test ./ui/rest/... ./infrastructure/chatwoot/... ./infrastructure/whatsapp/...
```

## Commit
Direto no `main`, mensagem estilo:
```
fix(chatwoot): quote inbound contact messages in agent replies

resolveReplyMessageID cai para o source_id (WAID:<id>) da mensagem citada
via API do Chatwoot quando não há link local, cobrindo mensagens do
cliente (incoming) que não têm linha em chatwoot_message_links.
```
Terminar com a linha `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.

## Build + Deploy
1. Rebuild da imagem (mesmo esquema das tags `gowa-pr739:*`). Sugestão de tag: `gowa-pr739:quote-fix` (ou o hash curto do novo commit).
2. **Redeploy via Dokploy API** (NUNCA docker/SSH direto — regra do projeto) nos 4 composes zap em `2.xlinx.com.br`: `zap8586`, `zap9273`, `zapvig`, `zap5827`. Atualizar a imagem no compose e disparar deploy.
   - `zap9273` é o que o usuário usa ativamente no Chatwoot → testar nele primeiro.

## Validação em produção (pós-deploy)
1. No Chatwoot do `zap9273`, responder com "reply" citando uma **mensagem do cliente** → confirmar que chega no WhatsApp **com** o balãozinho de citação.
2. Repetir citando uma mensagem enviada pela conta (deve continuar funcionando via link local).
3. Testar com anexo (imagem/áudio/doc) + citação.
4. Conferir logs sem novos erros de "Failed to fetch source_id".

## Riscos / notas
- Paginação do endpoint de mensagens do Chatwoot (ver nota na Alteração 2).
- Latência: o fallback faz 1 GET extra à API do Chatwoot só quando não há link local — aceitável (só em replies a mensagens sem link).
- A imagem em produção continua sendo buildada de working tree; manter tudo commitado no `main` após o fix.

## Contexto de servidores
- Composes zap em `2.xlinx.com.br` (Dokploy). Ver memórias: `dokploy-compose-api`, `dokploy-org-map`, `feedback-dokploy-workflow`, `feedback-dokploy-domains`.
- Fork local: `~/Desenvolvimento/gowa`, branch `main`. HEAD atual do fix: partir de `0acdc86`.

---

## Resolução final (2026-07-18)

A abordagem geral do plano (fallback pelo `source_id` da API do Chatwoot) estava
**certa**, mas a implementação inicial tinha **dois bugs** que só apareceram testando
em produção. Corrigidos em cima do fix original.

### Cronologia dos deploys (todos SÓ no `zap9273`, via Dokploy API; imagem buildada local → `docker save|ssh|docker load` → `compose.update`+`compose.deploy`)
| Imagem | Commit | O que trouxe | Resultado |
|--------|--------|--------------|-----------|
| `gowa-pr739:quote-fix`  | `2e7c95f` | fix original do plano (Alterações 1–3) | ❌ não citava cliente — decode quebrava |
| `gowa-pr739:quote-fix2` | `3dde024` | corrige decode numérico | ❌ ainda não citava — só lia 1ª página |
| `gowa-pr739:quote-fix3` | `69ac2e8` | pagina o endpoint de mensagens | ✅ **funciona** (validado pelo usuário) |

### Bug 1 — `message_type` numérico (corrigido em `3dde024`)
O `GetMessageSourceID` decodificava a lista de mensagens em `[]Message`, cujo campo é
`MessageType string`. Mas o endpoint **REST** `GET /conversations/{id}/messages` do
Chatwoot devolve `message_type` como **número** (0=incoming, 1=outgoing, 2=activity) —
ao contrário do **webhook**, onde vem string ("incoming"/"outgoing"). O decode do payload
inteiro falhava com `json: cannot unmarshal number into ... message_type of type string`,
o fallback retornava erro e a mensagem saía sem citação. **Correção:** decodificar num
struct local mínimo `{id, source_id}`, ignorando `message_type`/`content_type`.

### Bug 2 — só lia a primeira página (corrigido em `69ac2e8`) — o gargalo real
Confirmado inspecionando os dados de produção via API do Chatwoot: mensagens **incoming
TÊM** `source_id="WAID:<id>"` (a premissa do plano estava correta). A mensagem que o
agente cita, porém, costuma estar **muitas páginas atrás** — o endpoint retorna ~20 msgs
por página, mais novas primeiro. O `GetMessageSourceID` original só lia a 1ª página, não
achava a mensagem citada, retornava `""` → sem citação. **Correção:** paginar pra trás
via `?before=<menor_id_visto>` até achar a mensagem, ultrapassá-la (ids são monotônicos na
conversa), esvaziar, ou bater o teto de **40 páginas** (~800 msgs). Prova nos dados reais:
a msg citada `485784` resolve para `WAID:3EB009C40CC0BD7330E5A0` na 2ª página.

Observação que derrubou uma hipótese falsa: `source_id` vem `null` nas mensagens **criadas
no próprio Chatwoot** (respostas do agente digitadas lá), mas vem **preenchido** nas
**espelhadas do WhatsApp** (incoming e outgoing-do-telefone). Uma amostra só de respostas
do agente levou a concluir erradamente "source_id não é gravado" — a lição é **paginar e
inspecionar dados reais** antes de concluir.

### Testes de regressão adicionados
- `infrastructure/chatwoot/client_methods_test.go` → `TestGetMessageSourceID`: servidor
  `httptest` paginado com `message_type` **numérico** e a mensagem-alvo (com `source_id`)
  numa **página posterior**. Cobre os dois bugs de uma vez (falharia em ambas as versões
  anteriores). Suíte `./ui/rest/... ./infrastructure/chatwoot/...` verde; `gofmt` limpo.

### Gotchas operacionais aprendidos (registrados na memória `gowa-builds`)
- **Reconnect pós-deploy:** ao recriar o container, o auto-connect do WhatsApp pode falhar
  por corrida do DNS embutido do Docker (`127.0.0.11:53 server misbehaving`); o GOWA não faz
  retry → device fica offline. Conferir conexões (`netstat | grep ESTABLISHED` nas portas
  443/5222) e, se 0, reconectar via `GET https://<zap>.2.xlinx.com.br/app/reconnect` (Basic
  Auth, device já pareado, sem QR).
- **Transferência de imagem** (`ssh + docker load`) exige aprovação explícita do usuário a
  cada vez (o classificador de segurança bloqueia por padrão).

### Pendências / follow-ups possíveis (não bloqueiam)
- Deploy do mesmo fix nas outras instâncias (zap8586/zap5827/zapvig) — hoje NÃO têm o fix.
- Melhoria de fundo: fazer o forwarder gravar `chatwoot_message_link` também para mensagens
  **incoming** (hoje só grava outgoing). Isso faria o caminho local rápido (path 1) resolver
  sem depender de paginar a API — elimina a latência do fallback e o teto de 40 páginas.
- Latência: replies a mensagens muito antigas fazem vários GETs de paginação; aceitável hoje.

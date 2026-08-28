# Auditoria de acesso anônimo ao `/admin`

Data da verificação: 2026-08-28.

Sem cookies, usando `curl` com cache-bypass contra `https://subtitle.oldagesubs.com.br/admin?anoncheck=...`, o domínio respondeu `HTTP/2 303`, `Location: /login?...`, `Cache-Control: no-store` e `cf-cache-status: BYPASS`. O corpo não continha marcadores administrativos como e-mail, painel, projetos ou formulário de upload.

Uma sessão independente do navegador também foi redirecionada visualmente para `https://subtitle.oldagesubs.com.br/login?next=...` e apresentou somente o formulário de e-mail e senha.

Conclusão: a rota atualmente publicada não entrega o painel a um cliente sem sessão. Se o usuário vê o painel em uma aba supostamente anônima, a hipótese mais provável é que a aba esteja autenticada por uma extensão/gerenciador, que a captura tenha sido feita antes do último deploy ou que o endereço acessado tenha outro host/caminho. A validação objetiva é abrir exatamente `/admin?anoncheck=<valor-novo>` e observar o redirecionamento para `/login`.

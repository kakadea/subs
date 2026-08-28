# Guia de Operação e Instalação

Este guia ensina como instalar, atualizar e manter a plataforma `subs` no seu servidor com HestiaCP e Docker. O código fica em um checkout no próprio servidor; as atualizações fazem `git fetch/reset`, recompilam somente o serviço Go e sobem somente o container da aplicação. O HestiaCP permanece na frente.

## 1. Instalação Inicial

No servidor, a instalação inicial parte do checkout que já está no diretório de trabalho. O instalador cria o `.env`, faz o build local da aplicação, sobe a stack, instala o template proxy e configura o domínio no HestiaCP:

```bash
sudo mkdir -p /opt/subs
sudo git clone https://github.com/kakadea/subs.git /opt/subs/src
cd /opt/subs/src
chmod +x scripts/*.sh
sudo ./scripts/install-hestia.sh legenda.seudominio.com usuario_hestia
```

O instalador perguntará apenas a URL pública, o e-mail e a senha do administrador na primeira execução. Ele gera os demais segredos automaticamente, cria o arquivo `.env` com permissão restrita (`600`) e deixa o upstream pronto em `127.0.0.1:18180`.

Se o checkout ainda não existir, coloque o repositório no diretório de trabalho usando o método de acesso Git já configurado no servidor. A partir daí, não há login de registry, token de pacote ou GitHub CLI no ciclo de deploy.

## 2. Como Atualizar o app

O padrão de atualização é o mesmo usado no laboratório: atualize o checkout, faça o build somente do serviço Go e recrie somente esse container. Nginx interno, MariaDB, volumes e configuração do Hestia não são tocados.

```bash
cd /opt/subs/src
sudo git fetch origin main
sudo git reset --hard origin/main
sudo ./scripts/deploy.sh
```

O script `deploy.sh` executa, na prática:

```bash
sudo docker compose --env-file /opt/subs/src/.env build app
sudo docker compose --env-file /opt/subs/src/.env up -d --no-deps --force-recreate app
```

Para acompanhar o resultado:

```bash
sudo docker compose --env-file /opt/subs/src/.env -f /opt/subs/src/docker-compose.yml logs --no-color --since=3m app
```

Não há `docker login`, GHCR, PAT ou GitHub CLI nesse ciclo.

### Limites de recursos

O `docker-compose.yml` limita cada serviço para não herdar o limite amplo de memória do host. A configuração atual é:

| Serviço | Memória máxima | Swap | CPU máxima |
|---|---:|---:|---:|
| `app` | 256 MB | 0 MB adicional | 0,25 CPU |
| `nginx-files` | 128 MB | 0 MB adicional | 0,30 CPU |
| `mariadb` | 256 MB | 0 MB adicional | 0,25 CPU |

`memswap_limit` igual a `mem_limit` impede que cada container use swap adicional além do limite de memória. Esses limites valem somente para os três containers do `subs`; os serviços do restante do VPS não são alterados. O MariaDB continua com seu volume persistente, e o limite não apaga dados.

## 3. Fluxo do catálogo

O painel agora trabalha com projetos de anime. Em **Criar projeto**, cole uma URL HTTPS do MyAnimeList no formato `https://myanimelist.net/anime/2076/Kindaichi_Shounen_no_Jikenbo`. O servidor valida o domínio e o caminho, consulta o provedor configurado em `METADATA_API_BASE_URL` e salva o título, a capa, o número de episódios e o link do MAL no MariaDB.

Depois de criado, abra o projeto e use **Adicionar legendas**. O formulário permite selecionar vários arquivos de uma vez; cada arquivo é validado individualmente. Arquivos válidos entram na biblioteca mesmo que outro arquivo do lote esteja duplicado ou inválido, e o painel mostra o resumo das ocorrências. Por padrão, são aceitos até 20 arquivos por lote, com 25 MB por arquivo e 100 MB no total. Temporada e episódio não fazem mais parte do cadastro. Novos arquivos devem ser cadastrados dentro de um projeto.

Dentro do painel administrativo do projeto, a seção **Fontes** permite cadastrar somente uma URL HTTPS. Use-a para links de download dos episódios, arquivos de fonte `.ttf` ou outras páginas úteis relacionadas ao projeto. O sistema mostra apenas o link e deriva internamente o domínio para manter compatibilidade com registros antigos. As fontes só aparecem na página pública quando o projeto estiver compartilhado; projetos privados e suas fontes ficam restritos ao admin. A remoção do link é definitiva no banco.

O catálogo público lista projetos, não arquivos soltos. A página pública do projeto possui as abas **Legendas** e **Fontes**. A fonte padrão dos metadados é a Tenrai, uma API pública com esquema compatível com os campos necessários da Jikan/MAL; a URL do provedor fica configurável para permitir substituição futura.

## 4. Configuração no HestiaCP

O instalador configura automaticamente o proxy do domínio usando o template customizado `subs`, com upstream em `http://127.0.0.1:18180`. Ele usa a CLI oficial `v-add-web-domain-proxy` para domínios sem proxy e `v-change-web-domain-proxy-tpl` para domínios que já possuem proxy. Depois disso, as atualizações normais não reaplicam o template nem reiniciam o Hestia; só recriam o container `app`.

Se quiser conferir no painel, o domínio deve estar com **Proxy Support** ativo. O certificado TLS continua sendo gerenciado pelo HestiaCP; o container não publica HTTPS próprio.

## 5. Boas Práticas lá dentro

### Segurança
- **Nunca versionar o `.env`:** Ele contém as senhas do banco e a chave de sessão. O `.gitignore` e o `.dockerignore` já estão configurados para não enviá-lo ao Git nem para dentro da imagem.
- **Permissões:** O arquivo `/opt/subs/src/.env` deve ser `600`, preferencialmente pertencente a `root:root`. Assim, usuários comuns não conseguem lê-lo. Pessoas com acesso `root` ou permissão administrativa sobre o Docker podem, por definição, acessar segredos do servidor; essa é uma limitação do modelo de host e não deve ser confundida com exposição pública.
- **Exposição web:** O checkout em `/opt/subs/src` não é o `public_html` do Hestia. O Nginx interno recebe o volume de legendas somente como leitura e mantém `/protected/` como `internal`; essa rota não pode ser chamada diretamente pela Internet. O Nginx não serve o arquivo `.env`, e o `.env` também não é incluído na imagem Docker.
- **Cache de páginas:** As páginas SSR dinâmicas, incluindo `/p/{id}`, `/admin` e `/login`, enviam `Cache-Control: no-store`, `CDN-Cache-Control: no-store`, `Surrogate-Control: no-store` e `Vary: Cookie`. Isso evita que uma camada de proxy reutilize HTML antigo ou uma resposta renderizada com sessão administrativa.
- **Senha inicial:** `ADMIN_EMAIL` e `ADMIN_PASSWORD` servem somente para criar o primeiro administrador. Depois que já existe um admin, editar essas variáveis não troca a senha armazenada no MariaDB.
- **Usuário não-root:** A aplicação Go dentro do Docker roda como usuário `subs`, não como `root`.

### Troca da senha administrativa

Não altere `ADMIN_PASSWORD` esperando que a senha existente seja substituída. Use o script abaixo, que pede a senha interativamente e a envia pelo stdin, sem colocá-la na linha de comando, no histórico do shell ou nos logs:

```bash
cd /opt/subs/src
sudo ./scripts/change-admin-password.sh
```

A senha deve ter pelo menos 12 caracteres e pode conter espaços e caracteres como `#`, `=`, `$`, `!` e `@`. O comando grava somente o hash bcrypt no MariaDB, invalida as sessões administrativas existentes e não imprime a senha. Após confirmar que o novo login funciona, as duas variáveis de bootstrap podem ser removidas sem afetar a conta:

```bash
sudo sed -i '/^ADMIN_PASSWORD=/d; /^ADMIN_EMAIL=/d' /opt/subs/src/.env
sudo chmod 600 /opt/subs/src/.env
```

Esse último passo é opcional, mas reduz a quantidade de segredos operacionais mantidos no ambiente do container.

### Downloads

Os endpoints `/download/{id}` e `/l/{token}` passam pela autorização da aplicação. O app entrega o arquivo com `Content-Length`, `Content-Disposition`, suporte a Range e `Cache-Control: private, no-store`; não há dependência do `X-Accel-Redirect` ou do path `/protected/`.

### Organização de Arquivos
- **Storage:** As legendas ficam no volume `subtitles_data`. Não tente acessá-las pela pasta `public_html` do Hestia; elas são privadas.
- **Nomes:** O sistema renomeia os arquivos para o hash SHA-256 deles. Isso evita conflitos de nomes e ataques de path traversal.

### Banco de Dados
- **Metadata:** O MariaDB guarda o índice e a exclusão pelo Painel Administrativo é definitiva: o registro é apagado do banco, os links temporários associados são removidos por cascata e o arquivo é removido do volume de storage. Não há restauração automática.
- **Backup:**
    ```bash
    # Exemplo de backup do banco; use a senha quando solicitado
    docker compose exec -T mariadb mariadb-dump -u subs -p subs > backup.sql
    ```

## 6. Comandos Úteis

- **Ver logs:** `docker compose logs -f app`
- **Ver status:** `docker compose ps`
- **Reiniciar tudo:** `docker compose restart`
- **Entrar no banco:** `docker compose exec mariadb mariadb -u subs -p`

---

**Dica de Ouro:** Se você mudar de servidor, prepare o checkout em `/opt/subs/src`, restaure o `.env` e os volumes Docker e execute o instalador inicial uma vez. Depois, as atualizações seguem apenas por `git fetch/reset`, `build app` e `up -d --no-deps app`.


## Referência oficial usada para automação do HestiaCP

A CLI oficial do HestiaCP fornece `v-add-web-domain-proxy USER DOMAIN PROXY_TPL [RESTART]` para adicionar uma configuração de proxy sem sobrescrever as configurações existentes e `v-change-web-domain-proxy-tpl USER DOMAIN TEMPLATE [EXTENSIONS] [RESTART]` para trocar o template de proxy. A instalação inicial usa essas operações com o template customizado `subs`; as atualizações do app não mexem nelas. Referência: https://hestiacp.com/docs/reference/cli

O Hestia mantém os templates em `/usr/local/hestia/data/templates/web/nginx/` e recomenda copiar/criar templates próprios, pois rebuilds e atualizações podem sobrescrever templates padrão. Referência: https://hestiacp.com/docs/server-administration/web-templates

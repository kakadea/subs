# Guia de Operação e Instalação

Este guia ensina como instalar, atualizar e manter a plataforma `subs` no seu servidor com HestiaCP e Docker. O código fica em um checkout no próprio servidor; as atualizações fazem `git fetch/reset`, recompilam somente o serviço Go e sobem somente o container da aplicação. O HestiaCP permanece na frente.

## 1. Instalação Inicial

No servidor, a instalação inicial parte do checkout que já está no diretório de trabalho. O instalador cria o `.env`, faz o build local da aplicação, sobe a stack, instala o template proxy e configura o domínio no HestiaCP:

```bash
sudo mkdir -p /opt/subs
sudo git clone https://github.com/kakadea/subs.git /opt/subs/src
cd /opt/subs/src
chmod +x scripts/*.sh
sudo ./scripts/install.sh legenda.seudominio.com usuario_hestia
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
sudo docker compose --env-file /opt/subs/src/.env up -d --no-deps app
```

Para acompanhar o resultado:

```bash
sudo docker compose --env-file /opt/subs/src/.env -f /opt/subs/src/docker-compose.yml logs --no-color --since=3m app
```

Não há `docker login`, GHCR, PAT ou GitHub CLI nesse ciclo.

## 3. Configuração no HestiaCP

O instalador configura automaticamente o proxy do domínio usando o template customizado `subs`, com upstream em `http://127.0.0.1:18180`. Ele usa a CLI oficial `v-add-web-domain-proxy` para domínios sem proxy e `v-change-web-domain-proxy-tpl` para domínios que já possuem proxy. Depois disso, as atualizações normais não reaplicam o template nem reiniciam o Hestia; só recriam o container `app`.

Se quiser conferir no painel, o domínio deve estar com **Proxy Support** ativo. O certificado TLS continua sendo gerenciado pelo HestiaCP; o container não publica HTTPS próprio.

## 4. Boas Práticas lá dentro

### Segurança
- **Nunca versionar o `.env`:** Ele contém as senhas do banco e a chave de sessão. O `.gitignore` já está configurado para protegê-lo.
- **Permissões:** O arquivo `.env` deve ser `600` (apenas seu usuário lê).
- **Usuário não-root:** A aplicação Go dentro do Docker roda como usuário `subs`, não como `root`.

### Organização de Arquivos
- **Storage:** As legendas ficam no volume `subtitles_data`. Não tente acessá-las pela pasta `public_html` do Hestia; elas são privadas.
- **Nomes:** O sistema renomeia os arquivos para o hash SHA-256 deles. Isso evita conflitos de nomes e ataques de path traversal.

### Banco de Dados
- **Metadata:** O MariaDB guarda apenas o "índice". Se você precisar deletar algo permanentemente, use o Painel Administrativo (que faz o delete lógico).
- **Backup:**
    ```bash
    # Exemplo de backup do banco; use a senha quando solicitado
    docker compose exec -T mariadb mariadb-dump -u subs -p subs > backup.sql
    ```

## 5. Comandos Úteis

- **Ver logs:** `docker compose logs -f app`
- **Ver status:** `docker compose ps`
- **Reiniciar tudo:** `docker compose restart`
- **Entrar no banco:** `docker compose exec mariadb mariadb -u subs -p`

---

**Dica de Ouro:** Se você mudar de servidor, prepare o checkout em `/opt/subs/src`, restaure o `.env` e os volumes Docker e execute o instalador inicial uma vez. Depois, as atualizações seguem apenas por `git fetch/reset`, `build app` e `up -d --no-deps app`.


## Referência oficial usada para automação do HestiaCP

A CLI oficial do HestiaCP fornece `v-add-web-domain-proxy USER DOMAIN PROXY_TPL [RESTART]` para adicionar uma configuração de proxy sem sobrescrever as configurações existentes e `v-change-web-domain-proxy-tpl USER DOMAIN TEMPLATE [EXTENSIONS] [RESTART]` para trocar o template de proxy. A instalação inicial usa essas operações com o template customizado `subs`; as atualizações do app não mexem nelas. Referência: https://hestiacp.com/docs/reference/cli

O Hestia mantém os templates em `/usr/local/hestia/data/templates/web/nginx/` e recomenda copiar/criar templates próprios, pois rebuilds e atualizações podem sobrescrever templates padrão. Referência: https://hestiacp.com/docs/server-administration/web-templates

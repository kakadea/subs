# Guia de Operação e Instalação

Este guia ensina como instalar, atualizar e manter a plataforma `subs` no seu servidor com HestiaCP e Docker. **A imagem da aplicação é construída automaticamente no GitHub Actions e publicada no GitHub Container Registry; no servidor você faz apenas o pull e sobe os containers.**

## 1. Instalação Inicial

No seu servidor (via SSH):

```bash
# 1. Clone o repositório
gh repo clone kakadea/subs
cd subs

# 2. Torne os scripts executáveis
chmod +x scripts/*.sh

# 3. Gere o ambiente (responda as perguntas)
./scripts/bootstrap-env.sh

# 4. Inicie a plataforma
./scripts/deploy.sh
```

O script `bootstrap-env.sh` gera segredos fortes automaticamente e cria o arquivo `.env` com permissão restrita (`600`).

## 2. Como Atualizar (Sem `cp` e sem build local)

Para atualizar a plataforma quando houver código novo:

```bash
git pull origin main
./scripts/deploy.sh
```

O script `deploy.sh` valida a configuração, executa `docker compose pull` para baixar a imagem pronta do GHCR e reinicia os containers de forma limpa. O servidor não compila a aplicação. O build acontece automaticamente no GitHub Actions quando há push na branch `main`.

## 3. Acesso ao GHCR no servidor

Como o repositório é privado, a primeira instalação precisa autenticar o Docker no GitHub Container Registry. Crie um token clássico do GitHub com a permissão `read:packages` e execute uma única vez no servidor:

```bash
echo 'SEU_TOKEN_READ_PACKAGES' | docker login ghcr.io -u SEU_USUARIO --password-stdin
```

Se o usuário ainda não tiver permissão para usar o Docker sem `sudo`, use `sudo docker login ...` e também `sudo ./scripts/deploy.sh`. Nunca coloque o token no `.env`, no repositório ou em comandos salvos no histórico. Depois disso, o `./scripts/deploy.sh` fará apenas pull da imagem publicada.

## 4. Configuração no HestiaCP

O Docker está rodando internamente na porta `8081`. Você precisa dizer ao HestiaCP para mandar o tráfego do seu domínio para lá.

1.  No HestiaCP, vá em **Web** -> **Seu Domínio** -> **Edit**.
2.  Habilite **Proxy Support**.
3.  O ideal é criar um template customizado (veja `deploy/hestia/README.md`), mas para um teste rápido, você pode usar o template `default` e garantir que o domínio aponte para `http://127.0.0.1:8081`.

## 5. Boas Práticas lá dentro

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

## 6. Comandos Úteis

- **Ver logs:** `docker compose logs -f app`
- **Ver status:** `docker compose ps`
- **Reiniciar tudo:** `docker compose restart`
- **Entrar no banco:** `docker compose exec mariadb mariadb -u subs -p`

---

**Dica de Ouro:** Se você mudar de servidor, clone o repositório novamente, rode o bootstrap uma vez, autentique o Docker no GHCR e restaure os dados/volumes. O `.env` continua separado do código e não precisa ser recriado a cada deploy.

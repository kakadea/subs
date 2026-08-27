# Subs

Plataforma enxuta para armazenamento, organização e distribuição privada de arquivos de legenda. A aplicação foi implementada em **Go**, com execução em Docker e integração com HestiaCP/Nginx.

## Arquitetura

```text
Internet
   ↓
HestiaCP / Nginx do host
   ↓ proxy local para 127.0.0.1:8081
Nginx interno no Docker
   ├── proxy → Go API :8080
   └── /protected → storage privado
                         ↓
                      MariaDB
```

O HestiaCP termina TLS e administra o domínio. O Nginx interno faz o proxy da aplicação e entrega arquivos autorizados. A API Go cuida da autenticação, catálogo, uploads, metadados, links temporários e auditoria. O MariaDB guarda somente dados estruturados. O storage nunca é webroot.

## Funcionalidades implementadas

A interface pública permite pesquisar e abrir detalhes das legendas públicas. O painel administrativo permite login, upload de SRT/ASS/SSA/VTT/SUB, preenchimento de metadados, seleção de visibilidade, listagem, remoção lógica e criação de links temporários.

Os uploads são limitados por tamanho, validados por extensão e conteúdo textual, gravados em diretórios derivados do SHA-256 e armazenados com nome interno. Sessões usam tokens aleatórios cujo hash é persistido no banco, senhas usam bcrypt, cookies são HttpOnly/SameSite, formulários administrativos usam CSRF e a aplicação envia headers básicos de hardening.

Downloads não são transmitidos pelo processo Go. A API valida o acesso e responde com `X-Accel-Redirect`; o Nginx interno então entrega os bytes a partir do volume privado.

## Execução local com Docker

Na primeira instalação em um servidor com HestiaCP, use o checkout que já está no diretório de trabalho. O instalador cria o `.env`, faz o build local do app, sobe a stack e configura o proxy do domínio no HestiaCP:

```bash
sudo mkdir -p /opt/subs
sudo git clone https://github.com/kakadea/subs.git /opt/subs/src
cd /opt/subs/src
chmod +x scripts/*.sh
sudo ./scripts/install-hestia.sh legenda.seudominio.com usuario_hestia
```

O arquivo `.env` fica persistido no servidor e não é sobrescrito. O guia detalhado de instalação, HestiaCP, atualização, backup e operação está em [`docs/OPERACAO.md`](docs/OPERACAO.md).

Acesse `https://seu-dominio/` pelo HestiaCP. Para desenvolvimento direto, use `BASE_URL=http://localhost:8080` e `COOKIE_SECURE=false` no `.env`, e abra `http://127.0.0.1:8080`.

A primeira inicialização cria o usuário administrador definido por `ADMIN_EMAIL` e `ADMIN_PASSWORD`. Se já existir um administrador, os valores de bootstrap não alteram a conta existente. Depois do primeiro bootstrap, essas duas variáveis podem ser removidas do `.env`; as credenciais persistidas são os hashes bcrypt gravados no MariaDB.

Para trocar a senha sem editar o `.env` e sem expor a nova senha no histórico do shell, execute no checkout do servidor:

```bash
cd /opt/subs/src
sudo ./scripts/change-admin-password.sh
```

O script pede o e-mail, a senha duas vezes e aceita caracteres especiais, inclusive espaços, `#`, `=` e `$`. A nova senha é enviada apenas por stdin ao container, todas as sessões anteriores são invalidadas e será necessário fazer login novamente.

## Como buildar

O fluxo de atualização é direto no servidor e recompila somente o serviço Go:

```bash
cd /opt/subs/src
sudo git fetch origin main
sudo git reset --hard origin/main
sudo ./scripts/deploy.sh
```

O script executa `docker compose build app` e `docker compose up -d --no-deps app`. Nginx, MariaDB, volumes e configuração do Hestia não são tocados.

## Integração com HestiaCP

O Compose publica somente `127.0.0.1:18180`, conforme a recomendação de não deixar a API exposta. No domínio do Hestia, use um template Nginx customizado que encaminhe todas as requisições para `http://127.0.0.1:18180`.

O exemplo está em [`deploy/hestia/README.md`](deploy/hestia/README.md). Não edite os templates padrão do Hestia; atualizações e rebuilds podem sobrescrevê-los. Crie uma cópia customizada, habilite-a no domínio e reconstrua a configuração quando necessário.

## Operação

```bash
docker compose ps
docker compose logs -f app
docker compose logs -f nginx-files
curl -fsS https://seu-dominio/healthz
```

Para backup, copie o volume `mariadb_data` usando uma rotina consistente do MariaDB e mantenha uma cópia do volume `subtitles_data`. O `.env` contém segredos, deve ficar fora do Git e com permissão `600`; ele não fica dentro da área pública do Hestia nem é servido pelo Nginx.

## Estrutura

```text
cmd/server/                  entrypoint do serviço Go
internal/config/             configuração e validação de ambiente
internal/httpapp/            rotas, segurança e renderização
internal/store/              SQL, modelos e persistência
internal/webassets/          templates e CSS embutidos
migrations/                  schema SQL explícito
deploy/nginx/                Nginx interno e entrega protegida
deploy/hestia/               instruções de proxy do HestiaCP
docker-compose.yml           app, Nginx interno, MariaDB e volumes
Dockerfile                   build multiestágio
```

## Verificação

```bash
go test ./...
go vet ./...
go build ./cmd/server
```

O build de produção usa uma imagem multiestágio e executa a aplicação como usuário não root. Antes de publicar, valide a integração no servidor real, principalmente limites de upload, permissões dos volumes, proxy do Hestia, backup e downloads concorrentes.

## Decisões de infraestrutura

Não foram adicionados Redis, filas, Kubernetes, microserviços ou storage externo na primeira versão. A prioridade é manter poucos componentes e deixar a entrega de arquivos no Nginx. Esses serviços podem ser considerados depois, somente se métricas reais justificarem a complexidade.

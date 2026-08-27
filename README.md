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

```bash
cp .env.example .env
# edite .env e substitua todos os valores replace-with-*
chmod 600 .env
docker compose up -d --build
```

Acesse `https://seu-dominio/` pelo HestiaCP. Para desenvolvimento direto, use `BASE_URL=http://localhost:8081` e `COOKIE_SECURE=false` no `.env`, e abra `http://127.0.0.1:8081`.

A primeira inicialização cria o usuário administrador definido por `ADMIN_EMAIL` e `ADMIN_PASSWORD`. Se já existir um administrador, os valores de bootstrap não alteram a conta existente.

## Integração com HestiaCP

O Compose publica somente `127.0.0.1:8081`, conforme a recomendação de não deixar a API exposta. No domínio do Hestia, use um template Nginx customizado que encaminhe todas as requisições para `http://127.0.0.1:8081`.

O exemplo está em [`deploy/hestia/README.md`](deploy/hestia/README.md). Não edite os templates padrão do Hestia; atualizações e rebuilds podem sobrescrevê-los. Crie uma cópia customizada, habilite-a no domínio e reconstrua a configuração quando necessário.

## Operação

```bash
docker compose ps
docker compose logs -f app
docker compose logs -f nginx-files
curl -fsS https://seu-dominio/healthz
```

Para backup, copie o volume `mariadb_data` usando uma rotina consistente do MariaDB e mantenha uma cópia do volume `subtitles_data`. O `.env` contém segredos e nunca deve ser versionado.

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

# Integração com HestiaCP

O HestiaCP deve permanecer responsável pelo domínio, certificado TLS e proxy reverso público. A aplicação não deve abrir uma porta pública própria.

## 1. Subir o Compose

Na máquina que hospeda o HestiaCP:

```bash
cp .env.example .env
# edite .env e defina segredos fortes
chmod 600 .env
docker compose up -d --build
```

O Compose publica somente `127.0.0.1:8081` para o Nginx interno. A API Go e o MariaDB ficam somente na rede Docker.

## 2. Proxy no domínio do Hestia

Crie um template Nginx customizado do Hestia, em vez de editar o template padrão. O Hestia pode reconstruir os arquivos do domínio durante atualizações ou rebuilds.

O bloco de proxy conceitual é:

```nginx
location / {
    proxy_pass http://127.0.0.1:8081;
    proxy_http_version 1.1;
    proxy_buffering off;
    proxy_request_buffering off;
    proxy_connect_timeout 5s;
    proxy_send_timeout 300s;
    proxy_read_timeout 300s;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-Host $host;
}
```

Ajuste o template `.tpl` e `.stpl` conforme o modo HTTP e HTTPS do domínio. Ative-o no domínio e reconstrua a configuração pelo painel ou por `v-rebuild-user`.

## 3. Storage e X-Accel-Redirect

O Nginx do Hestia não deve receber o volume privado. O Nginx interno do Compose possui o volume como leitura e entrega o arquivo somente depois que a API Go autoriza a requisição.

A location interna está definida em `deploy/nginx/default.conf`:

```nginx
location /protected/ {
    internal;
    alias /data/;
    autoindex off;
}
```

Uma requisição pública direta para `/protected/` deve retornar 404. O fluxo válido é `/download/{id}` ou `/l/{token}`; a API então responde com `X-Accel-Redirect`.

## 4. Verificação

```bash
curl -fsS https://subs.example.com/healthz
curl -i https://subs.example.com/protected/subtitles/test
ss -ltnp | grep 8081
```

O healthcheck deve responder `{"status":"ok"}`. O path protegido deve ser inacessível diretamente. A porta 8081 deve estar ligada apenas a `127.0.0.1`.

## Observações

O Hestia deve terminar TLS e encaminhar o header `X-Forwarded-Proto`. O cookie seguro pode permanecer habilitado porque o navegador acessa o domínio por HTTPS, mesmo que o proxy entre Hestia e Docker use HTTP local.

Não abra portas 3306, 8080 ou a porta do Nginx interno para `0.0.0.0`. Não coloque o diretório `storage` em `public_html`.

# Integração com HestiaCP

O HestiaCP permanece responsável pelo domínio, certificado TLS e proxy reverso público. A aplicação `subs` roda localmente em Docker e não abre porta pública própria.

## Instalação recomendada

No checkout já existente no servidor, execute como root:

```bash
cd /opt/subs/src
chmod +x scripts/*.sh
sudo ./scripts/install-hestia.sh subtitle.oldagesubs.com.br cloud
```

O instalador cria/instala os templates persistentes `subs.tpl` e `subs.stpl`, sobe MariaDB, constrói a imagem Go localmente, inicia o app e o Nginx interno e configura o domínio do usuário Hestia informado. Não há login de registry, GHCR, PAT, chave SSH ou GitHub CLI necessários no ciclo normal.

A topologia é:

```text
Internet / Cloudflare
        ↓ HTTPS
HestiaCP Nginx do host
        ↓ http://127.0.0.1:18180
Nginx interno Docker
        ↓ http://app:8080
Go app ─── MariaDB somente na rede Docker
   └── rede de saída Docker exclusiva → Tenrai/MAL
```

O binding `127.0.0.1:18180` é local ao servidor. O Nginx interno encaminha para o app e mantém o volume privado montado somente como leitura para a guarda interna de compatibilidade; a entrega atual é validada e transmitida pelo próprio app. Para criar projetos, somente o app possui uma segunda rede Docker de saída, usada para consultar a API pública de metadados; MariaDB e Nginx não entram nessa rede e nenhuma porta adicional é publicada.

## Atualização normal

Depois que o código for publicado no repositório público, a atualização seletiva é:

```bash
cd /opt/subs/src
sudo git fetch origin main
sudo git reset --hard origin/main
sudo ./scripts/deploy.sh
```

O script executa somente `docker compose build app` e `docker compose up -d --no-deps app`. MariaDB, volume de dados, Nginx interno e configuração do Hestia não são recriados.

## Proxy persistente

Os templates ficam em `/usr/local/hestia/data/templates/web/nginx/` e usam `proxy_pass http://127.0.0.1:18180`. Não edite diretamente os arquivos de vhost gerados em `/home/cloud/conf/web/`; o Hestia pode reconstruí-los. Se o domínio mostrar a página `We're working on it!`, o template efetivo ainda não é `subs` ou o vhost não foi reconstruído. Verifique:

```bash
sudo /usr/local/hestia/bin/v-list-web-domain cloud subtitle.oldagesubs.com.br json
sudo grep -R "proxy_pass http://127.0.0.1:18180" /home/cloud/conf/web/subtitle.oldagesubs.com.br /etc/nginx 2>/dev/null
sudo nginx -t
```

Se o primeiro comando confirmar o domínio e o `grep` não encontrar o upstream, reaplique somente o template do domínio:

```bash
sudo /usr/local/hestia/bin/v-change-web-domain-proxy-tpl cloud subtitle.oldagesubs.com.br subs "jpg,jpeg,gif,png,webp,ico,svg,css,js,zip,tgz,gz,rar,bz2,doc,xls,exe,pdf,ppt,txt,odt,ods,odp,odf,tar,wav,bmp,rtf,mp3,avi,mpeg,flv,html,htm,srt,ass,ssa,vtt,sub" yes && sudo /usr/local/hestia/bin/v-restart-proxy yes
```

Depois, valide o proxy e a origem:

```bash
curl -i --max-time 10 http://127.0.0.1:18180/healthz
curl -skI --max-time 15 "https://subtitle.oldagesubs.com.br/healthz?check=$(date +%s)"
```

A primeira resposta deve vir do Nginx interno com `200` e `{"status":"ok"}`. A segunda deve mostrar a aplicação, não a página padrão do Hestia. Enquanto o primeiro teste local não responder, não adianta limpar cache da Cloudflare.

## Downloads

Os endpoints públicos são `/download/{id}` para a legenda pública e `/l/{token}` para links temporários gerados pelo painel. O app responde com `Content-Length`, `Content-Disposition`, suporte a Range e `Cache-Control: private, no-store`. Não existe mais dependência operacional do caminho `/protected/` ou de `X-Accel-Redirect`.

## Segurança

O arquivo `/opt/subs/src/.env` deve ficar fora do `public_html`, com proprietário `root:root` e permissão `600`. O checkout não é servido pelo vhost e o `.env` não é copiado para a imagem. Usuários com acesso root ou controle administrativo do Docker no host continuam capazes de acessar os segredos, como em qualquer aplicação hospedada nesse servidor.

Não abra as portas 3306, 8080 ou 18180 para `0.0.0.0`. Não use `docker compose down -v`, `docker system prune -a` ou `docker volume prune` no VPS compartilhado.

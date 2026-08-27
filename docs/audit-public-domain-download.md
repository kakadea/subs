# Auditoria pública — 27/08/2026

A consulta `curl -skS` ao endpoint informado pelo usuário `https://subtitle.oldagesubs.com.br/download/27f2bd9ff947734a628f58f6b4257df1` respondeu HTTP/2 404, `server: cloudflare`, `cf-cache-status: MISS`, content-type `text/plain; charset=utf-8`. Portanto, naquele momento não era possível atribuir essa resposta a cache antigo da Cloudflare.

A abertura de `https://subtitle.oldagesubs.com.br/` no navegador exibiu a página HestiaCP `Coming Soon` / `We're working on it!`, não a aplicação Subs. Isso indica que o vhost público ainda não está encaminhando o domínio para `127.0.0.1:18180`, ou que o vhost efetivo do domínio não é o template `subs`. Antes de diagnosticar o download no app, é necessário corrigir/verificar o proxy Hestia.

O screenshot do usuário mostra um endereço `/download/<public_id>` e erro `ERR_INVALID_RESPONSE`. No código atual, `/download/<public_id>` é o download público estável da legenda; o botão administrativo `Link` cria um token temporário diferente em `/l/<token>` a cada solicitação. O fluxo por projeto deve preservar download estável da legenda e tratar links temporários separadamente.

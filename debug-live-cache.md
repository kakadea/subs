# Diagnóstico de cache em produção — 28/08/2026

A página `https://subtitle.oldagesubs.com.br/p/9b48750cfc4b02c9f6161599229e95f3?check=1fe9b1e` respondeu com o layout atual, abas `Legendas 7` e `Fontes 0`, além das sete legendas públicas.

A mesma URL sem querystring, `https://subtitle.oldagesubs.com.br/p/9b48750cfc4b02c9f6161599229e95f3`, respondeu com layout antigo, sem abas, `0 legendas disponíveis` e `0 arquivos`.

Conclusão: as sete legendas existem e a aplicação atual as entrega; o HTML antigo está sendo servido por cache no caminho sem querystring, provavelmente na camada Cloudflare/proxy. A correção deve enviar `Cache-Control: no-store` também para páginas dinâmicas públicas `/p/` (idealmente todas as páginas SSR não-estáticas), além do deploy e teste com querystring para validação imediata.

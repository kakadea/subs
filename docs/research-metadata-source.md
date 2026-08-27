# Fonte de metadados MAL — pesquisa em 27/08/2026

A documentação pública da Jikan v4 existe em https://docs.api.jikan.moe/ e a API historicamente expõe dados de anime por ID, incluindo título, imagens e episódios.

A issue https://github.com/jikan-me/jikan-rest/issues/610 registra que a API pública Jikan será descontinuada em 1º de outubro de 2026 e menciona Tenrai como alternativa pública. A mesma issue relata erros 504 intermitentes da Jikan em endpoints de anime e comentários de usuários que migraram para Tenrai.

Decisão preliminar: não acoplar o catálogo permanentemente à Jikan. A implementação deve aceitar e validar a URL canônica do MyAnimeList, extrair o ID numérico, obter nome/capa/episódios em uma camada de provedor substituível e persistir os metadados no MariaDB. A camada deve poder usar uma API pública estável ou a própria página pública do MAL como fallback, sem expor a URL externa diretamente para o navegador.

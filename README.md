intalar extensão protobuf VSC

rodar comando `go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest`

Gerar arquivo de configuração do banco de dados:
- ir para o diretório raiz do projeto
- rodar comando `sqlc generate -f ./server/internal/server/db/config/sqlc.yml`
module example-postgresql

go 1.23.2

require (
	github.com/medatechnology/goutil v0.0.7
	github.com/medatechnology/simpleorm v0.0.4
	github.com/medatechnology/suresql v0.0.0
)

replace github.com/medatechnology/suresql => ../../

require (
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/lib/pq v1.10.9 // indirect
)

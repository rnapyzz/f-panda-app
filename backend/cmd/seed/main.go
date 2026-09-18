// Command seed creates (or updates the password of) an office_admin user,
// so there is a way to log in on a freshly migrated database.
//
// Usage: seed -email admin@example.com -name "Admin" -password secret
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"

	_ "github.com/go-sql-driver/mysql"

	"github.com/rnapyzz/f-panda-app/backend/internal/auth"
	"github.com/rnapyzz/f-panda-app/backend/internal/config"
	"github.com/rnapyzz/f-panda-app/backend/internal/db"
)

func main() {
	email := flag.String("email", "", "admin email (required)")
	name := flag.String("name", "", "admin display name (required)")
	password := flag.String("password", "", "admin password (required)")
	flag.Parse()

	if *email == "" || *name == "" || *password == "" {
		log.Fatal("all of -email, -name, -password are required")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	sqlDB, err := sql.Open("mysql", cfg.DBDSN)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer sqlDB.Close()

	hash, err := auth.HashPassword(*password)
	if err != nil {
		log.Fatalf("hash password: %v", err)
	}

	ctx := context.Background()
	queries := db.New(sqlDB)

	id, err := queries.CreateUser(ctx, db.CreateUserParams{
		Email:        *email,
		Name:         *name,
		Role:         db.AppUserRoleOfficeAdmin,
		PasswordHash: hash,
	})
	if err != nil {
		log.Fatalf("create user: %v", err)
	}

	fmt.Printf("created office_admin user id=%d email=%s\n", id, *email)
}

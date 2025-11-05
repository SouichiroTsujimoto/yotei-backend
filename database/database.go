package database

import (
	"fmt"
	"log"
	"os"
	"time"

	"yotei-backend/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// Connect はデータベースに接続する
func Connect() error {
	var err error

	// 環境変数からデータベースURLを取得
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL environment variable is not set")
	}

	// 時間を空けて何度か試す
	for i := 0; i < 4; i++ {
		DB, err = gorm.Open(postgres.Open(databaseURL), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Info),
		})
		if err == nil {
			break
		}
		time.Sleep(20 * time.Second)
	}
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	log.Println("Database connection established")
	return nil
}

// Migrate はデータベースのマイグレーションを実行する
func Migrate() error {
	log.Println("Running database migrations...")

	err := DB.AutoMigrate(
		&models.Event{},
		&models.CandidateDate{},
		&models.Participant{},
		&models.Response{},
		&models.RSSFeed{},
	)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	log.Println("Database migrations completed successfully")
	return nil
}

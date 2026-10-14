package main

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required; copy backend/.env.example and set the environment variable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("database unavailable: %v", err)
	}

	r := gin.Default()
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})
	r.Use(stage5Guard())
	if err := registerStage5(r, pool); err != nil {
		log.Fatalf("initialize authentication: %v", err)
	}
	r.GET("/api/v1/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "bloodconnect-api"}) })
	r.GET("/api/v1/health/db", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "database": "connected"})
	})

	// Create the donor table automatically for this learning-stage application.
	schema := `CREATE TABLE IF NOT EXISTS donors (
 id BIGSERIAL PRIMARY KEY,
 full_name TEXT NOT NULL,
 blood_group VARCHAR(3) NOT NULL CHECK (blood_group IN ('A+','A-','B+','B-','AB+','AB-','O+','O-')),
 city TEXT NOT NULL,
 email TEXT NOT NULL UNIQUE,
 phone TEXT NOT NULL,
 available BOOLEAN NOT NULL DEFAULT TRUE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
 )`
	if _, err := pool.Exec(context.Background(), schema); err != nil {
		log.Fatalf("create donors table: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		ALTER TABLE app_users ADD COLUMN IF NOT EXISTS donor_id BIGINT REFERENCES donors(id);
		CREATE UNIQUE INDEX IF NOT EXISTS app_users_donor_id_key ON app_users(donor_id) WHERE donor_id IS NOT NULL;
		UPDATE app_users u SET donor_id=d.id FROM donors d
		WHERE u.role='donor' AND u.donor_id IS NULL AND LOWER(u.email)=LOWER(d.email);
	`); err != nil {
		log.Fatalf("link donor accounts to donor profiles: %v", err)
	}
	type Donor struct {
		ID         int64     `json:"id"`
		FullName   string    `json:"full_name"`
		BloodGroup string    `json:"blood_group"`
		City       string    `json:"city"`
		Email      string    `json:"email"`
		Phone      string    `json:"phone"`
		Available  bool      `json:"available"`
		CreatedAt  time.Time `json:"created_at"`
	}
	type DonorInput struct {
		FullName   string `json:"full_name"`
		BloodGroup string `json:"blood_group"`
		City       string `json:"city"`
		Email      string `json:"email"`
		Phone      string `json:"phone"`
		Available  bool   `json:"available"`
	}
	validBlood := map[string]bool{"A+": true, "A-": true, "B+": true, "B-": true, "AB+": true, "AB-": true, "O+": true, "O-": true}
	validate := func(d DonorInput) string {
		if len(strings.TrimSpace(d.FullName)) < 2 {
			return "Full name must be at least 2 characters"
		}
		if !validBlood[d.BloodGroup] {
			return "Invalid blood group"
		}
		if strings.TrimSpace(d.City) == "" {
			return "City is required"
		}
		if !strings.Contains(d.Email, "@") || !strings.Contains(d.Email, ".") {
			return "Valid email required"
		}
		if strings.TrimSpace(d.Phone) == "" {
			return "Phone is required"
		}
		return ""
	}
	r.GET("/api/v1/donors/me", func(c *gin.Context) {
		userID, ok := c.Get("user_id")
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Login required"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		var d Donor
		err := pool.QueryRow(ctx, `SELECT d.id,d.full_name,d.blood_group,d.city,d.email,d.phone,d.available,d.created_at
			FROM donors d JOIN app_users u ON u.donor_id=d.id
			WHERE u.id=$1 AND u.role='donor'`, userID).Scan(&d.ID, &d.FullName, &d.BloodGroup, &d.City, &d.Email, &d.Phone, &d.Available, &d.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Donor profile not found; contact an administrator"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load donor profile"})
			return
		}
		c.JSON(http.StatusOK, d)
	})
	r.PUT("/api/v1/donors/me", func(c *gin.Context) {
		userID, ok := c.Get("user_id")
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Login required"})
			return
		}
		var in struct {
			FullName   string `json:"full_name"`
			BloodGroup string `json:"blood_group"`
			City       string `json:"city"`
			Phone      string `json:"phone"`
			Available  bool   `json:"available"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		var email string
		err := pool.QueryRow(ctx, `SELECT email FROM app_users WHERE id=$1 AND role='donor'`, userID).Scan(&email)
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Donor account not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load donor account"})
			return
		}
		if msg := validate(DonorInput{FullName: in.FullName, BloodGroup: in.BloodGroup, City: in.City, Email: email, Phone: in.Phone}); msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}
		var d Donor
		err = pool.QueryRow(ctx, `UPDATE donors d SET full_name=$1,blood_group=$2,city=$3,phone=$4,available=$5
			FROM app_users u WHERE u.id=$6 AND u.donor_id=d.id AND u.role='donor'
			RETURNING d.id,d.full_name,d.blood_group,d.city,d.email,d.phone,d.available,d.created_at`,
			strings.TrimSpace(in.FullName), in.BloodGroup, strings.TrimSpace(in.City), strings.TrimSpace(in.Phone), in.Available, userID).
			Scan(&d.ID, &d.FullName, &d.BloodGroup, &d.City, &d.Email, &d.Phone, &d.Available, &d.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Donor profile not found; contact an administrator"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not update donor profile"})
			return
		}
		c.JSON(http.StatusOK, d)
	})
	r.GET("/api/v1/donors", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		q := `SELECT id, full_name, blood_group, city, email, phone, available, created_at FROM donors WHERE ($1 = '' OR blood_group = $1) AND ($2 = '' OR city ILIKE '%' || $2 || '%') AND ($3 = '' OR full_name ILIKE '%' || $3 || '%') ORDER BY id DESC LIMIT 200`
		rows, err := pool.Query(ctx, q, c.Query("blood_group"), c.Query("city"), c.Query("name"))
		if err != nil {
			c.JSON(500, gin.H{"error": "Database query failed"})
			return
		}
		defer rows.Close()
		donors := make([]Donor, 0)
		for rows.Next() {
			var d Donor
			if err := rows.Scan(&d.ID, &d.FullName, &d.BloodGroup, &d.City, &d.Email, &d.Phone, &d.Available, &d.CreatedAt); err != nil {
				c.JSON(500, gin.H{"error": "Database read failed"})
				return
			}
			donors = append(donors, d)
		}
		if rows.Err() != nil {
			c.JSON(500, gin.H{"error": "Database iteration failed"})
			return
		}
		c.JSON(200, donors)
	})
	r.POST("/api/v1/donors", func(c *gin.Context) {
		var in DonorInput
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(400, gin.H{"error": "Invalid JSON"})
			return
		}
		if msg := validate(in); msg != "" {
			c.JSON(400, gin.H{"error": msg})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		var d Donor
		err := pool.QueryRow(ctx, `INSERT INTO donors (full_name,blood_group,city,email,phone,available) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id,full_name,blood_group,city,email,phone,available,created_at`, strings.TrimSpace(in.FullName), in.BloodGroup, strings.TrimSpace(in.City), strings.ToLower(strings.TrimSpace(in.Email)), strings.TrimSpace(in.Phone), in.Available).Scan(&d.ID, &d.FullName, &d.BloodGroup, &d.City, &d.Email, &d.Phone, &d.Available, &d.CreatedAt)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				c.JSON(409, gin.H{"error": "Email already registered"})
				return
			}
			c.JSON(500, gin.H{"error": "Failed to create donor"})
			return
		}
		c.JSON(201, d)
	})
	r.PUT("/api/v1/donors/:id", func(c *gin.Context) {
		if !requireAdmin(c) {
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(400, gin.H{"error": "Invalid donor ID"})
			return
		}
		var in DonorInput
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(400, gin.H{"error": "Invalid JSON"})
			return
		}
		if msg := validate(in); msg != "" {
			c.JSON(400, gin.H{"error": msg})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		tx, err := pool.Begin(ctx)
		if err != nil {
			c.JSON(500, gin.H{"error": "Could not begin donor update"})
			return
		}
		defer tx.Rollback(ctx)
		var d Donor
		email := strings.ToLower(strings.TrimSpace(in.Email))
		err = tx.QueryRow(ctx, `UPDATE donors SET full_name=$1,blood_group=$2,city=$3,email=$4,phone=$5,available=$6 WHERE id=$7 RETURNING id,full_name,blood_group,city,email,phone,available,created_at`, strings.TrimSpace(in.FullName), in.BloodGroup, strings.TrimSpace(in.City), email, strings.TrimSpace(in.Phone), in.Available, id).Scan(&d.ID, &d.FullName, &d.BloodGroup, &d.City, &d.Email, &d.Phone, &d.Available, &d.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(404, gin.H{"error": "Donor not found"})
			return
		}
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				c.JSON(409, gin.H{"error": "Email already registered"})
				return
			}
			c.JSON(500, gin.H{"error": "Failed to update donor"})
			return
		}
		_, err = tx.Exec(ctx, `UPDATE app_users SET email=$1 WHERE donor_id=$2 AND role='donor'`, email, id)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				c.JSON(409, gin.H{"error": "Email already belongs to an account"})
				return
			}
			c.JSON(500, gin.H{"error": "Failed to update donor login email"})
			return
		}
		if err = tx.Commit(ctx); err != nil {
			c.JSON(500, gin.H{"error": "Failed to save donor changes"})
			return
		}
		c.JSON(200, d)
	})
	r.DELETE("/api/v1/donors/:id", func(c *gin.Context) {
		if !requireAdmin(c) {
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(400, gin.H{"error": "Invalid donor ID"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		tx, err := pool.Begin(ctx)
		if err != nil {
			c.JSON(500, gin.H{"error": "Could not begin donor deletion"})
			return
		}
		defer tx.Rollback(ctx)
		_, err = tx.Exec(ctx, `DELETE FROM app_users WHERE donor_id=$1 AND role='donor'`, id)
		if err != nil {
			c.JSON(500, gin.H{"error": "Could not remove linked donor login"})
			return
		}
		tag, err := tx.Exec(ctx, `DELETE FROM donors WHERE id=$1`, id)
		if err != nil {
			c.JSON(500, gin.H{"error": "Could not delete donor"})
			return
		}
		if tag.RowsAffected() == 0 {
			c.JSON(404, gin.H{"error": "Donor not found"})
			return
		}
		if err = tx.Commit(ctx); err != nil {
			c.JSON(500, gin.H{"error": "Could not save donor deletion"})
			return
		}
		c.JSON(200, gin.H{"status": "deleted"})
	})
	if err := registerStage3(r, pool); err != nil {
		log.Fatalf("initialize stage 3 tables: %v", err)
	}
	if err := registerStage4(r, pool); err != nil {
		log.Fatalf("initialize stage 4 tables: %v", err)
	}
	log.Printf("API listening at http://localhost:%s", port)
	log.Fatal(r.Run(":" + port))
}

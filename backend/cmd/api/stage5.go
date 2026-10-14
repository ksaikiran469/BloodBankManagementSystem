package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type tokenClaims struct {
	Sub  int64  `json:"sub"`
	Role string `json:"role"`
	Exp  int64  `json:"exp"`
}

var jwtKey []byte
var bootstrapAdminID int64

func requireAdmin(c *gin.Context) bool {
	if c.GetString("role") != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Admin only"})
		return false
	}
	return true
}

func tokenFor(id int64, role string) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(tokenClaims{Sub: id, Role: role, Exp: time.Now().Add(8 * time.Hour).Unix()})
	if err != nil {
		return "", err
	}
	data := header + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, jwtKey)
	mac.Write([]byte(data))
	return data + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func verifyToken(token string) (tokenClaims, error) {
	var c tokenClaims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return c, errors.New("invalid token")
	}
	if parts[0] != base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`)) {
		return c, errors.New("invalid token header")
	}
	mac := hmac.New(sha256.New, jwtKey)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return c, errors.New("invalid signature")
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || json.Unmarshal(b, &c) != nil || c.Sub <= 0 || time.Now().Unix() >= c.Exp {
		return c, errors.New("expired or invalid token")
	}
	if c.Role != "admin" && c.Role != "hospital" && c.Role != "donor" {
		return c, errors.New("invalid role")
	}
	return c, nil
}
func stage5Guard() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if c.Request.Method == http.MethodOptions || path == "/api/v1/health" || path == "/api/v1/health/db" || path == "/api/v1/auth/login" || path == "/api/v1/auth/register" {
			c.Next()
			return
		}
		raw := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if raw == "" || raw == c.GetHeader("Authorization") {
			c.AbortWithStatusJSON(401, gin.H{"error": "Login required"})
			return
		}
		claims, err := verifyToken(raw)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "Invalid or expired session"})
			return
		}
		c.Set("user_id", claims.Sub)
		c.Set("role", claims.Role)
		if claims.Role == "admin" && claims.Sub != bootstrapAdminID {
			c.AbortWithStatusJSON(403, gin.H{"error": "This administrator account is not enabled"})
			return
		}
		if claims.Role == "donor" {
			allowed := (path == "/api/v1/auth/me" && c.Request.Method == http.MethodGet) ||
				(path == "/api/v1/donors/me" && (c.Request.Method == http.MethodGet || c.Request.Method == http.MethodPut))
			if !allowed {
				c.AbortWithStatusJSON(403, gin.H{"error": "Donors may only access their own donor profile"})
				return
			}
			c.Next()
			return
		}
		// Role restrictions are enforced by the API, not merely hidden in React.
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			allowed := claims.Role == "admin" || (claims.Role == "hospital" && (strings.HasPrefix(path, "/api/v1/requests") || strings.HasPrefix(path, "/api/v1/inventory")))
			if !allowed {
				c.AbortWithStatusJSON(403, gin.H{"error": "Insufficient permissions for this action"})
				return
			}
		}
	}
}
func registerStage5(r *gin.Engine, pool *pgxpool.Pool) error {
	secret := os.Getenv("JWT_SECRET")
	if len(secret) < 32 {
		return errors.New("JWT_SECRET must be set to at least 32 characters")
	}
	jwtKey = []byte(secret)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS app_users (
 id BIGSERIAL PRIMARY KEY, email TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL,
 role TEXT NOT NULL CHECK (role IN ('admin','hospital','donor')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());`)
	if err != nil {
		return err
	}
	// The configured bootstrap account is the only enabled administrator.
	adminEmail := strings.ToLower(strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_EMAIL")))
	adminPassword := os.Getenv("BOOTSTRAP_ADMIN_PASSWORD")
	if len(adminEmail) > 254 || !strings.Contains(adminEmail, "@") || len(adminPassword) < 10 || len(adminPassword) > 72 {
		return errors.New("BOOTSTRAP_ADMIN_EMAIL must be valid and BOOTSTRAP_ADMIN_PASSWORD must be 10–72 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	err = pool.QueryRow(ctx, `INSERT INTO app_users(email,password_hash,role) VALUES($1,$2,'admin')
		ON CONFLICT(email) DO UPDATE SET password_hash=EXCLUDED.password_hash
		WHERE app_users.role='admin' RETURNING id`, adminEmail, string(hash)).Scan(&bootstrapAdminID)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("configured administrator email is already assigned to a non-admin account")
	}
	if err != nil {
		return err
	}
	r.POST("/api/v1/auth/register", func(c *gin.Context) {
		var in struct {
			Email      string `json:"email"`
			Password   string `json:"password"`
			FullName   string `json:"full_name"`
			BloodGroup string `json:"blood_group"`
			City       string `json:"city"`
			Phone      string `json:"phone"`
		}
		if c.ShouldBindJSON(&in) != nil {
			c.JSON(400, gin.H{"error": "Invalid input"})
			return
		}
		in.Email = strings.ToLower(strings.TrimSpace(in.Email))
		in.FullName = strings.TrimSpace(in.FullName)
		in.City = strings.TrimSpace(in.City)
		in.Phone = strings.TrimSpace(in.Phone)
		if len(in.Email) > 254 || !strings.Contains(in.Email, "@") || len(in.Password) < 12 || len(in.Password) > 72 ||
			len(in.FullName) < 2 || !bloodGroups[in.BloodGroup] || in.City == "" || in.Phone == "" {
			c.JSON(400, gin.H{"error": "Provide a valid donor profile, email and a password of 12–72 characters"})
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(500, gin.H{"error": "Registration failed"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		tx, err := pool.Begin(ctx)
		if err != nil {
			c.JSON(500, gin.H{"error": "Could not begin donor registration"})
			return
		}
		defer tx.Rollback(ctx)
		var donorID int64
		err = tx.QueryRow(ctx, `SELECT id FROM donors WHERE email=$1`, in.Email).Scan(&donorID)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `INSERT INTO donors(full_name,blood_group,city,email,phone)
				VALUES($1,$2,$3,$4,$5) RETURNING id`, in.FullName, in.BloodGroup, in.City, in.Email, in.Phone).Scan(&donorID)
		} else if err == nil {
			var matches bool
			err = tx.QueryRow(ctx, `SELECT LOWER(full_name)=LOWER($1) AND blood_group=$2 AND LOWER(city)=LOWER($3) AND phone=$4
				FROM donors WHERE id=$5`, in.FullName, in.BloodGroup, in.City, in.Phone, donorID).Scan(&matches)
			if err == nil && !matches {
				c.JSON(409, gin.H{"error": "Donor details do not match the existing profile; contact an administrator"})
				return
			}
		}
		if err != nil {
			c.JSON(409, gin.H{"error": "Donor profile could not be linked; verify the details and try again"})
			return
		}
		_, err = tx.Exec(ctx, `INSERT INTO app_users(email,password_hash,role,donor_id) VALUES($1,$2,'donor',$3)`, in.Email, string(hash), donorID)
		if err != nil {
			c.JSON(409, gin.H{"error": "Account could not be created (email may already have an account)"})
			return
		}
		if err = tx.Commit(ctx); err != nil {
			c.JSON(500, gin.H{"error": "Could not save donor account"})
			return
		}
		c.JSON(201, gin.H{"message": "Donor account created. Please log in."})
	})
	r.POST("/api/v1/auth/login", func(c *gin.Context) {
		var in struct {
			Email        string `json:"email"`
			Password     string `json:"password"`
			ExpectedRole string `json:"expected_role"`
		}
		if c.ShouldBindJSON(&in) != nil || (in.ExpectedRole != "admin" && in.ExpectedRole != "donor") {
			c.JSON(400, gin.H{"error": "Invalid input"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		var id int64
		var hash, role string
		err := pool.QueryRow(ctx, `SELECT id,password_hash,role FROM app_users WHERE email=$1`, strings.ToLower(strings.TrimSpace(in.Email))).Scan(&id, &hash, &role)
		loginEmail := strings.ToLower(strings.TrimSpace(in.Email))
		if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil || role != in.ExpectedRole ||
			(in.ExpectedRole == "admin" && loginEmail != adminEmail) {
			c.JSON(401, gin.H{"error": "Invalid email or password"})
			return
		}
		token, err := tokenFor(id, role)
		if err != nil {
			c.JSON(500, gin.H{"error": "Could not create session"})
			return
		}
		c.JSON(200, gin.H{"token": token, "role": role, "email": loginEmail})
	})
	r.GET("/api/v1/auth/me", func(c *gin.Context) {
		id, _ := c.Get("user_id")
		role, _ := c.Get("role")
		c.JSON(200, gin.H{"id": id, "role": role})
	})
	r.POST("/api/v1/auth/hospital-users", func(c *gin.Context) {
		if c.GetString("role") != "admin" {
			c.JSON(403, gin.H{"error": "Admin only"})
			return
		}
		var in struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if c.ShouldBindJSON(&in) != nil || len(in.Password) < 12 || len(in.Password) > 72 || !strings.Contains(in.Email, "@") {
			c.JSON(400, gin.H{"error": "Valid email and password (12–72 characters) required"})
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to create user"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		_, err = pool.Exec(ctx, `INSERT INTO app_users(email,password_hash,role) VALUES($1,$2,'hospital')`, strings.ToLower(strings.TrimSpace(in.Email)), string(hash))
		if err != nil {
			c.JSON(409, gin.H{"error": "Email already exists or invalid"})
			return
		}
		c.JSON(201, gin.H{"message": "Hospital user created"})
	})
	r.GET("/api/v1/notifications", func(c *gin.Context) {
		role := c.GetString("role")
		if role == "donor" {
			c.JSON(200, []gin.H{})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		out := make([]gin.H, 0)
		rows, err := pool.Query(ctx, `SELECT h.name,i.blood_group,i.units,i.low_threshold FROM blood_inventory i JOIN hospitals h ON h.id=i.hospital_id WHERE i.units<i.low_threshold ORDER BY i.units ASC LIMIT 30`)
		if err != nil {
			c.JSON(500, gin.H{"error": "Could not load notifications"})
			return
		}
		for rows.Next() {
			var name, group string
			var units, threshold int
			if rows.Scan(&name, &group, &units, &threshold) == nil {
				out = append(out, gin.H{"type": "low_stock", "message": fmt.Sprintf("%s: %s stock low (%d units; threshold %d)", name, group, units, threshold)})
			}
		}
		rows.Close()
		if role == "admin" || role == "hospital" {
			rows, err = pool.Query(ctx, `SELECT id,status FROM blood_requests WHERE status IN ('pending','approved') ORDER BY id DESC LIMIT 30`)
			if err == nil {
				for rows.Next() {
					var id int64
					var status string
					if rows.Scan(&id, &status) == nil {
						out = append(out, gin.H{"type": "request", "message": "Blood request #" + strconv.FormatInt(id, 10) + " is " + status})
					}
				}
				rows.Close()
			}
		}
		c.JSON(200, out)
	})
	// Compile-time use of pgx for a clear no-rows error contract.
	_ = pgx.ErrNoRows
	return nil
}

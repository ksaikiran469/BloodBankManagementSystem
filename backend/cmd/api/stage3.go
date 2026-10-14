package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type hospital struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	City  string `json:"city"`
	Email string `json:"email"`
	Phone string `json:"phone"`
}
type stock struct {
	ID           int64     `json:"id"`
	HospitalID   int64     `json:"hospital_id"`
	HospitalName string    `json:"hospital_name"`
	BloodGroup   string    `json:"blood_group"`
	Units        int       `json:"units"`
	LowThreshold int       `json:"low_threshold"`
	UpdatedAt    time.Time `json:"updated_at"`
}

var bloodGroups = map[string]bool{"A+": true, "A-": true, "B+": true, "B-": true, "AB+": true, "AB-": true, "O+": true, "O-": true}

func registerStage3(r *gin.Engine, pool *pgxpool.Pool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS hospitals (
 id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL, city TEXT NOT NULL, email TEXT NOT NULL UNIQUE, phone TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
 CREATE TABLE IF NOT EXISTS blood_inventory (
 id BIGSERIAL PRIMARY KEY, hospital_id BIGINT NOT NULL REFERENCES hospitals(id) ON DELETE RESTRICT,
 blood_group VARCHAR(3) NOT NULL CHECK (blood_group IN ('A+','A-','B+','B-','AB+','AB-','O+','O-')),
 units INTEGER NOT NULL DEFAULT 0 CHECK (units >= 0), low_threshold INTEGER NOT NULL DEFAULT 5 CHECK (low_threshold >= 0),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), UNIQUE(hospital_id,blood_group));`)
	if err != nil {
		return err
	}
	r.GET("/api/v1/hospitals", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		rows, err := pool.Query(ctx, "SELECT id,name,city,email,phone FROM hospitals ORDER BY id DESC LIMIT 200")
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to load hospitals"})
			return
		}
		defer rows.Close()
		out := make([]hospital, 0)
		for rows.Next() {
			var h hospital
			if err := rows.Scan(&h.ID, &h.Name, &h.City, &h.Email, &h.Phone); err != nil {
				c.JSON(500, gin.H{"error": "Failed to read hospital"})
				return
			}
			out = append(out, h)
		}
		if rows.Err() != nil {
			c.JSON(500, gin.H{"error": "Failed to read hospitals"})
			return
		}
		c.JSON(200, out)
	})
	r.POST("/api/v1/hospitals", func(c *gin.Context) {
		if !requireAdmin(c) {
			return
		}
		var h hospital
		if err := c.ShouldBindJSON(&h); err != nil {
			c.JSON(400, gin.H{"error": "Invalid JSON"})
			return
		}
		h.Name = strings.TrimSpace(h.Name)
		h.City = strings.TrimSpace(h.City)
		h.Email = strings.ToLower(strings.TrimSpace(h.Email))
		h.Phone = strings.TrimSpace(h.Phone)
		if len(h.Name) < 2 || h.City == "" || !strings.Contains(h.Email, "@") || h.Phone == "" {
			c.JSON(400, gin.H{"error": "Name, city, valid email and phone are required"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		err := pool.QueryRow(ctx, "INSERT INTO hospitals(name,city,email,phone) VALUES($1,$2,$3,$4) RETURNING id", h.Name, h.City, h.Email, h.Phone).Scan(&h.ID)
		if err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				c.JSON(409, gin.H{"error": "Hospital email already exists"})
				return
			}
			c.JSON(500, gin.H{"error": "Failed to create hospital"})
			return
		}
		c.JSON(201, h)
	})
	r.PUT("/api/v1/hospitals/:id", func(c *gin.Context) {
		if !requireAdmin(c) {
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(400, gin.H{"error": "Invalid hospital ID"})
			return
		}
		var h hospital
		if err := c.ShouldBindJSON(&h); err != nil {
			c.JSON(400, gin.H{"error": "Invalid JSON"})
			return
		}
		h.Name = strings.TrimSpace(h.Name)
		h.City = strings.TrimSpace(h.City)
		h.Email = strings.ToLower(strings.TrimSpace(h.Email))
		h.Phone = strings.TrimSpace(h.Phone)
		if len(h.Name) < 2 || h.City == "" || !strings.Contains(h.Email, "@") || h.Phone == "" {
			c.JSON(400, gin.H{"error": "Name, city, valid email and phone are required"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		err = pool.QueryRow(ctx, `UPDATE hospitals SET name=$1,city=$2,email=$3,phone=$4 WHERE id=$5
			RETURNING id,name,city,email,phone`, h.Name, h.City, h.Email, h.Phone, id).
			Scan(&h.ID, &h.Name, &h.City, &h.Email, &h.Phone)
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(404, gin.H{"error": "Hospital not found"})
			return
		}
		if err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				c.JSON(409, gin.H{"error": "Hospital email already exists"})
				return
			}
			c.JSON(500, gin.H{"error": "Failed to update hospital"})
			return
		}
		c.JSON(200, h)
	})
	r.DELETE("/api/v1/hospitals/:id", func(c *gin.Context) {
		if !requireAdmin(c) {
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(400, gin.H{"error": "Invalid hospital ID"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		tag, err := pool.Exec(ctx, `DELETE FROM hospitals WHERE id=$1`, id)
		if err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23503" {
				c.JSON(409, gin.H{"error": "Cannot delete a hospital while it has inventory or blood requests"})
				return
			}
			c.JSON(500, gin.H{"error": "Failed to delete hospital"})
			return
		}
		if tag.RowsAffected() == 0 {
			c.JSON(404, gin.H{"error": "Hospital not found"})
			return
		}
		c.JSON(200, gin.H{"status": "deleted"})
	})
	r.GET("/api/v1/inventory", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		rows, err := pool.Query(ctx, `SELECT i.id,i.hospital_id,h.name,i.blood_group,i.units,i.low_threshold,i.updated_at FROM blood_inventory i JOIN hospitals h ON h.id=i.hospital_id ORDER BY h.name,i.blood_group LIMIT 500`)
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to load inventory"})
			return
		}
		defer rows.Close()
		out := make([]stock, 0)
		for rows.Next() {
			var s stock
			if err := rows.Scan(&s.ID, &s.HospitalID, &s.HospitalName, &s.BloodGroup, &s.Units, &s.LowThreshold, &s.UpdatedAt); err != nil {
				c.JSON(500, gin.H{"error": "Failed to read inventory"})
				return
			}
			out = append(out, s)
		}
		if rows.Err() != nil {
			c.JSON(500, gin.H{"error": "Failed to read inventory"})
			return
		}
		c.JSON(200, out)
	})
	r.POST("/api/v1/inventory", func(c *gin.Context) {
		var in struct {
			HospitalID   int64  `json:"hospital_id"`
			BloodGroup   string `json:"blood_group"`
			Units        int    `json:"units"`
			LowThreshold int    `json:"low_threshold"`
		}
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(400, gin.H{"error": "Invalid JSON"})
			return
		}
		if in.HospitalID <= 0 || !bloodGroups[in.BloodGroup] || in.Units < 0 || in.LowThreshold < 0 {
			c.JSON(400, gin.H{"error": "Invalid hospital, blood group, units or threshold"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		_, err := pool.Exec(ctx, `INSERT INTO blood_inventory(hospital_id,blood_group,units,low_threshold) VALUES($1,$2,$3,$4) ON CONFLICT(hospital_id,blood_group) DO UPDATE SET units=EXCLUDED.units,low_threshold=EXCLUDED.low_threshold,updated_at=NOW()`, in.HospitalID, in.BloodGroup, in.Units, in.LowThreshold)
		if err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23503" {
				c.JSON(404, gin.H{"error": "Hospital not found"})
				return
			}
			c.JSON(500, gin.H{"error": "Failed to save inventory"})
			return
		}
		c.JSON(200, gin.H{"status": "saved"})
	})
	r.PUT("/api/v1/inventory/:id", func(c *gin.Context) {
		if !requireAdmin(c) {
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(400, gin.H{"error": "Invalid inventory ID"})
			return
		}
		var in struct {
			Units        int `json:"units"`
			LowThreshold int `json:"low_threshold"`
		}
		if err := c.ShouldBindJSON(&in); err != nil || in.Units < 0 || in.LowThreshold < 0 {
			c.JSON(400, gin.H{"error": "Units and threshold must be non-negative"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		tag, err := pool.Exec(ctx, `UPDATE blood_inventory SET units=$1,low_threshold=$2,updated_at=NOW() WHERE id=$3`,
			in.Units, in.LowThreshold, id)
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to update inventory"})
			return
		}
		if tag.RowsAffected() == 0 {
			c.JSON(404, gin.H{"error": "Inventory item not found"})
			return
		}
		c.JSON(200, gin.H{"status": "updated"})
	})
	r.DELETE("/api/v1/inventory/:id", func(c *gin.Context) {
		if !requireAdmin(c) {
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(400, gin.H{"error": "Invalid inventory ID"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		tag, err := pool.Exec(ctx, `DELETE FROM blood_inventory WHERE id=$1`, id)
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to delete inventory"})
			return
		}
		if tag.RowsAffected() == 0 {
			c.JSON(404, gin.H{"error": "Inventory item not found"})
			return
		}
		c.JSON(200, gin.H{"status": "deleted"})
	})
	r.POST("/api/v1/inventory/:id/adjust", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(400, gin.H{"error": "Invalid inventory ID"})
			return
		}
		var in struct {
			Delta int `json:"delta"`
		}
		if err := c.ShouldBindJSON(&in); err != nil || in.Delta == 0 || in.Delta > 100000 || in.Delta < -100000 {
			c.JSON(400, gin.H{"error": "Provide a nonzero delta within 100000 units"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		var units int
		err = pool.QueryRow(ctx, "UPDATE blood_inventory SET units=units+$1,updated_at=NOW() WHERE id=$2 AND units+$1>=0 RETURNING units", in.Delta, id).Scan(&units)
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusConflict, gin.H{"error": "Inventory not found or insufficient units"})
			return
		}
		if err != nil {
			c.JSON(500, gin.H{"error": "Could not adjust inventory"})
			return
		}
		c.JSON(200, gin.H{"units": units})
	})
	return nil
}

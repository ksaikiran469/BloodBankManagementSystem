package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type bloodRequest struct {
	ID           int64     `json:"id"`
	HospitalID   int64     `json:"hospital_id"`
	HospitalName string    `json:"hospital_name"`
	BloodGroup   string    `json:"blood_group"`
	Units        int       `json:"units"`
	Urgency      string    `json:"urgency"`
	Status       string    `json:"status"`
	Notes        string    `json:"notes"`
	CreatedAt    time.Time `json:"created_at"`
}

func registerStage4(r *gin.Engine, pool *pgxpool.Pool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS blood_requests (
 id BIGSERIAL PRIMARY KEY,
 hospital_id BIGINT NOT NULL REFERENCES hospitals(id),
 blood_group VARCHAR(3) NOT NULL CHECK (blood_group IN ('A+','A-','B+','B-','AB+','AB-','O+','O-')),
 units INTEGER NOT NULL CHECK (units > 0 AND units <= 10000),
 urgency TEXT NOT NULL CHECK (urgency IN ('routine','urgent','critical')),
 status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','fulfilled','rejected')),
 notes TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
 );`)
	if err != nil {
		return err
	}
	const query = `SELECT b.id,b.hospital_id,h.name,b.blood_group,b.units,b.urgency,b.status,b.notes,b.created_at FROM blood_requests b JOIN hospitals h ON h.id=b.hospital_id`
	scan := func(row pgx.Row, b *bloodRequest) error {
		return row.Scan(&b.ID, &b.HospitalID, &b.HospitalName, &b.BloodGroup, &b.Units, &b.Urgency, &b.Status, &b.Notes, &b.CreatedAt)
	}
	r.GET("/api/v1/requests", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		rows, err := pool.Query(ctx, query+` ORDER BY b.id DESC LIMIT 200`)
		if err != nil {
			c.JSON(500, gin.H{"error": "Could not load requests"})
			return
		}
		defer rows.Close()
		out := make([]bloodRequest, 0)
		for rows.Next() {
			var b bloodRequest
			if err := scan(rows, &b); err != nil {
				c.JSON(500, gin.H{"error": "Could not read requests"})
				return
			}
			out = append(out, b)
		}
		if rows.Err() != nil {
			c.JSON(500, gin.H{"error": "Could not read requests"})
			return
		}
		c.JSON(200, out)
	})
	r.POST("/api/v1/requests", func(c *gin.Context) {
		var in struct {
			HospitalID int64  `json:"hospital_id"`
			BloodGroup string `json:"blood_group"`
			Units      int    `json:"units"`
			Urgency    string `json:"urgency"`
			Notes      string `json:"notes"`
		}
		if c.ShouldBindJSON(&in) != nil || in.HospitalID <= 0 || !bloodGroups[in.BloodGroup] || in.Units <= 0 || in.Units > 10000 || (in.Urgency != "routine" && in.Urgency != "urgent" && in.Urgency != "critical") || len(in.Notes) > 1000 {
			c.JSON(400, gin.H{"error": "Invalid hospital, group, units, urgency or notes"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		var b bloodRequest
		var err error
		var id int64
		err = pool.QueryRow(ctx, `INSERT INTO blood_requests(hospital_id,blood_group,units,urgency,notes) VALUES($1,$2,$3,$4,$5) RETURNING id`, in.HospitalID, in.BloodGroup, in.Units, in.Urgency, strings.TrimSpace(in.Notes)).Scan(&id)
		if err != nil {
			c.JSON(400, gin.H{"error": "Unable to create request; check hospital"})
			return
		}
		err = scan(pool.QueryRow(ctx, query+` WHERE b.id=$1`, id), &b)
		if err != nil {
			c.JSON(500, gin.H{"error": "Request saved but could not be loaded"})
			return
		}
		c.JSON(201, b)
	})
	r.POST("/api/v1/requests/:id/status", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(400, gin.H{"error": "Invalid request ID"})
			return
		}
		var in struct {
			Status string `json:"status"`
		}
		if c.ShouldBindJSON(&in) != nil || (in.Status != "approved" && in.Status != "rejected" && in.Status != "fulfilled") {
			c.JSON(400, gin.H{"error": "Status must be approved, rejected or fulfilled"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
		defer cancel()
		tx, err := pool.Begin(ctx)
		if err != nil {
			c.JSON(500, gin.H{"error": "Transaction failed"})
			return
		}
		defer tx.Rollback(ctx)
		var hospitalID int64
		var group, status string
		var units int
		err = tx.QueryRow(ctx, `SELECT hospital_id,blood_group,units,status FROM blood_requests WHERE id=$1 FOR UPDATE`, id).Scan(&hospitalID, &group, &units, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(404, gin.H{"error": "Request not found"})
			return
		}
		if err != nil {
			c.JSON(500, gin.H{"error": "Could not read request"})
			return
		}
		valid := (status == "pending" && (in.Status == "approved" || in.Status == "rejected")) || (status == "approved" && in.Status == "fulfilled")
		if !valid {
			c.JSON(409, gin.H{"error": "Invalid status transition: pending → approved/rejected; approved → fulfilled"})
			return
		}
		if in.Status == "fulfilled" {
			tag, err := tx.Exec(ctx, `UPDATE blood_inventory SET units=units-$1,updated_at=NOW() WHERE hospital_id=$2 AND blood_group=$3 AND units >= $1`, units, hospitalID, group)
			if err != nil {
				c.JSON(500, gin.H{"error": "Could not update inventory"})
				return
			}
			if tag.RowsAffected() != 1 {
				c.JSON(409, gin.H{"error": "Insufficient inventory for this hospital and exact blood group"})
				return
			}
		}
		_, err = tx.Exec(ctx, `UPDATE blood_requests SET status=$1 WHERE id=$2`, in.Status, id)
		if err != nil {
			c.JSON(500, gin.H{"error": "Could not update status"})
			return
		}
		if err = tx.Commit(ctx); err != nil {
			c.JSON(500, gin.H{"error": "Could not commit changes"})
			return
		}
		c.JSON(200, gin.H{"id": id, "status": in.Status})
	})
	r.DELETE("/api/v1/requests/:id", func(c *gin.Context) {
		if !requireAdmin(c) {
			return
		}
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(400, gin.H{"error": "Invalid request ID"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		tag, err := pool.Exec(ctx, `DELETE FROM blood_requests WHERE id=$1 AND status='fulfilled'`, id)
		if err != nil {
			c.JSON(500, gin.H{"error": "Could not delete request"})
			return
		}
		if tag.RowsAffected() == 0 {
			var status string
			err := pool.QueryRow(ctx, `SELECT status FROM blood_requests WHERE id=$1`, id).Scan(&status)
			if errors.Is(err, pgx.ErrNoRows) {
				c.JSON(404, gin.H{"error": "Request not found"})
				return
			}
			if err != nil {
				c.JSON(500, gin.H{"error": "Could not verify request status"})
				return
			}
			c.JSON(409, gin.H{"error": "Only fulfilled requests can be deleted"})
			return
		}
		c.JSON(200, gin.H{"status": "deleted"})
	})
	r.GET("/api/v1/requests/:id/donors", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(400, gin.H{"error": "Invalid request ID"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		var group, city string
		err = pool.QueryRow(ctx, `SELECT b.blood_group,h.city FROM blood_requests b JOIN hospitals h ON h.id=b.hospital_id WHERE b.id=$1`, id).Scan(&group, &city)
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(404, gin.H{"error": "Request not found"})
			return
		}
		if err != nil {
			c.JSON(500, gin.H{"error": "Could not load request"})
			return
		}
		rows, err := pool.Query(ctx, `SELECT id,full_name,blood_group,city FROM donors WHERE available=true AND blood_group=$1 ORDER BY (LOWER(city)=LOWER($2)) DESC, id DESC LIMIT 30`, group, city)
		if err != nil {
			c.JSON(500, gin.H{"error": "Could not load donors"})
			return
		}
		defer rows.Close()
		out := make([]gin.H, 0)
		for rows.Next() {
			var did int64
			var name, bg, dcity string
			if err := rows.Scan(&did, &name, &bg, &dcity); err != nil {
				c.JSON(500, gin.H{"error": "Could not read donors"})
				return
			}
			out = append(out, gin.H{"id": did, "full_name": name, "blood_group": bg, "city": dcity, "same_city": strings.EqualFold(city, dcity)})
		}
		if rows.Err() != nil {
			c.JSON(500, gin.H{"error": "Could not read donors"})
			return
		}
		c.JSON(200, gin.H{"disclaimer": "Demo directory filter only; not a medical compatibility or eligibility determination.", "donors": out})
	})
	return nil
}

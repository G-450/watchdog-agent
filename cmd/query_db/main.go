package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	dbPath := "./data.db"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("Failed to open sqlite database: %v", err)
	}
	defer db.Close()

	// Query cluster snapshots count
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM cluster_snapshots").Scan(&count)
	if err != nil {
		log.Fatalf("Failed to query cluster_snapshots: %v", err)
	}
	fmt.Printf("Total Cluster Snapshots saved: %d\n\n", count)

	// Query workload snapshots
	fmt.Println("Recent Workload Snapshots:")
	fmt.Printf("%-20s %-20s %-30s %-10s %-10s\n", "TIMESTAMP", "NAMESPACE", "WORKLOAD", "REPLICAS", "CPU USAGE")
	fmt.Println("--------------------------------------------------------------------------------------------------")
	
	rows, err := db.Query("SELECT timestamp, namespace, name, replicas, cpu_usage FROM workload_snapshots ORDER BY id DESC LIMIT 10")
	if err != nil {
		log.Fatalf("Failed to query workload_snapshots: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var timestamp, namespace, name string
		var replicas int
		var cpuUsage float64
		if err := rows.Scan(&timestamp, &namespace, &name, &replicas, &cpuUsage); err != nil {
			log.Fatalf("Failed to scan row: %v", err)
		}
		fmt.Printf("%-20s %-20s %-30s %-10d %-10.4f\n", timestamp[:19], namespace, name, replicas, cpuUsage)
	}
}

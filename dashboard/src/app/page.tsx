"use client";

import { useEffect, useState } from "react";
import styles from "./page.module.css";

type Recommendation = {
  target: string;
  expected_savings: number;
  confidence_score: number;
  proposed_state: string;
};

type ClusterData = {
  snapshot: {
    total_cost: number;
    namespaces: Record<string, any>;
  };
  recommendations: Recommendation[];
  lastUpdated: string;
};

type Cluster = {
  id: string;
  data: ClusterData;
};

export default function Home() {
  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const fetchClusters = async () => {
      try {
        const res = await fetch("/api/clusters");
        const json = await res.json();
        setClusters(json.clusters || []);
      } catch (err) {
        console.error("Failed to fetch clusters", err);
      } finally {
        setLoading(false);
      }
    };

    fetchClusters();
    const interval = setInterval(fetchClusters, 5000); // Auto-refresh every 5s
    return () => clearInterval(interval);
  }, []);

  return (
    <main className={styles.container}>
      <header className={styles.header}>
        <h1 className={styles.title}>Watchdog Control Plane</h1>
        <div className={styles.badge}>
          <span className={styles.pulse}></span>
          Live Monitoring Active
        </div>
      </header>

      {loading ? (
        <div className={styles.emptyState}>
          <h3>Initializing AI Core...</h3>
          <p>Connecting to federated agents</p>
        </div>
      ) : clusters.length === 0 ? (
        <div className={styles.emptyState}>
          <h3>No Clusters Connected</h3>
          <p>Waiting for Watchdog Agents to send telemetry and AI recommendations.</p>
        </div>
      ) : (
        <div className={styles.grid}>
          {clusters.map((cluster) => {
            const data = cluster.data;
            const nsCount = Object.keys(data.snapshot?.namespaces || {}).length;
            const totalSavings = (data.recommendations || []).reduce((acc, r) => acc + (r.expected_savings || 0), 0);

            return (
              <div key={cluster.id} className={styles.card}>
                <h2 className={styles.cardTitle}>
                  {cluster.id} <span>Updated: {new Date(data.lastUpdated).toLocaleTimeString()}</span>
                </h2>

                <div className={styles.statRow}>
                  <div className={styles.statLabel}>Total Cluster Cost</div>
                  <div className={styles.statValue}>${data.snapshot?.total_cost?.toFixed(2) || "0.00"}</div>
                </div>

                <div className={styles.statRow}>
                  <div className={styles.statLabel}>Namespaces Profiled</div>
                  <div className={styles.statValue}>{nsCount}</div>
                </div>

                <div className={styles.statRow}>
                  <div className={styles.statLabel}>Potential AI Savings</div>
                  <div className={`${styles.statValue} ${styles.positive}`}>
                    ${totalSavings.toFixed(2)}/mo
                  </div>
                </div>

                <div className={styles.divider}></div>

                <div className={styles.recommendationList}>
                  <h3 style={{ fontSize: '1rem', marginBottom: '0.5rem' }}>Top Recommendations</h3>
                  {data.recommendations && data.recommendations.length > 0 ? (
                    data.recommendations.slice(0, 3).map((rec, i) => (
                      <div key={i} className={styles.recommendationItem}>
                        <div className={styles.recommendationHeader}>
                          <span className={styles.targetName}>{rec.target}</span>
                          <span className={styles.savings}>+${rec.expected_savings?.toFixed(2)}</span>
                        </div>
                        <div className={styles.recommendationBody}>
                          Proposed: {rec.proposed_state} <br/>
                          Confidence: {(rec.confidence_score * 100).toFixed(0)}%
                        </div>
                      </div>
                    ))
                  ) : (
                    <div style={{ color: 'var(--text-secondary)', fontSize: '0.875rem' }}>No pending recommendations.</div>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </main>
  );
}

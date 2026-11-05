-- Fairway Read-Only Analysis Queries
--
-- These queries are read-only SELECT statements designed to run against
-- the Fairway PostgreSQL database initialized with schema.sql.
-- None of these queries mutate table state.

--------------------------------------------------------------------------------
-- 1. Measurements per corridor
-- Returns total number of measurements recorded for each corridor.
--------------------------------------------------------------------------------
SELECT
    c.id AS corridor_id,
    c.name AS corridor_name,
    COUNT(m.id) AS measurement_count
FROM corridors c
LEFT JOIN measurements m ON c.id = m.corridor_id
GROUP BY c.id, c.name
ORDER BY c.name ASC;

--------------------------------------------------------------------------------
-- 2. Share of measurements per integrity_state per corridor
-- Computes the count and percentage share of measurements for each integrity state
-- ('usable', 'degraded', 'unusable', 'unknown') within each corridor.
--------------------------------------------------------------------------------
SELECT
    c.id AS corridor_id,
    c.name AS corridor_name,
    m.integrity_state,
    COUNT(m.id) AS state_count,
    ROUND(
        COUNT(m.id)::NUMERIC / NULLIF(SUM(COUNT(m.id)) OVER (PARTITION BY c.id), 0) * 100.0,
        2
    ) AS share_pct
FROM corridors c
JOIN measurements m ON c.id = m.corridor_id
GROUP BY c.id, c.name, m.integrity_state
ORDER BY c.name ASC, m.integrity_state ASC;

--------------------------------------------------------------------------------
-- 3. All state_change_events with corridor name and timestamps
-- Lists all historical corridor state transitions with corridor names and timestamps.
--------------------------------------------------------------------------------
SELECT
    e.id AS event_id,
    c.id AS corridor_id,
    c.name AS corridor_name,
    e.from_state,
    e.to_state,
    e.measurement_id,
    e.occurred_at,
    e.webhook_sent,
    e.webhook_sent_at
FROM state_change_events e
JOIN corridors c ON e.corridor_id = c.id
ORDER BY e.occurred_at DESC, e.id DESC;

--------------------------------------------------------------------------------
-- 4. Min, median, and max loss_pct per corridor
-- Calculates the minimum, median (50th percentile), and maximum loss percentage
-- for corridors where pathfinding succeeded and loss_pct was computed.
--------------------------------------------------------------------------------
SELECT
    c.id AS corridor_id,
    c.name AS corridor_name,
    COUNT(m.loss_pct) AS sample_count,
    MIN(m.loss_pct) AS min_loss_pct,
    ROUND(
        (PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY m.loss_pct))::NUMERIC,
        4
    ) AS median_loss_pct,
    MAX(m.loss_pct) AS max_loss_pct
FROM corridors c
JOIN measurements m ON c.id = m.corridor_id
WHERE m.loss_pct IS NOT NULL
GROUP BY c.id, c.name
ORDER BY c.name ASC;

--------------------------------------------------------------------------------
-- 5. Daily average loss_pct per corridor
-- Calculates daily average loss percentage per corridor over time.
--------------------------------------------------------------------------------
SELECT
    m.measured_at::DATE AS measurement_date,
    c.id AS corridor_id,
    c.name AS corridor_name,
    COUNT(m.loss_pct) AS sample_count,
    ROUND(AVG(m.loss_pct), 4) AS avg_loss_pct
FROM corridors c
JOIN measurements m ON c.id = m.corridor_id
WHERE m.loss_pct IS NOT NULL
GROUP BY m.measured_at::DATE, c.id, c.name
ORDER BY measurement_date DESC, c.name ASC;

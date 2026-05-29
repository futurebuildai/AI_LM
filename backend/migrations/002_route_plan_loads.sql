-- 002_route_plan_loads.sql
-- Multi-load CVRP: a route plan now splits a day's stops across multiple trucks
-- by weight capacity. Each load is one truck's sequenced stops + per-load totals
-- and becomes one delivery_route on write-back. Stored as JSONB alongside the
-- existing flattened `stops` union.
ALTER TABLE route_plans
    ADD COLUMN loads JSONB NOT NULL DEFAULT '[]'::jsonb;

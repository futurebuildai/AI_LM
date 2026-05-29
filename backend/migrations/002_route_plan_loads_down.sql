-- 002_route_plan_loads_down.sql
-- Manual rollback for 002_route_plan_loads.sql. The migrator skips *_down.sql
-- files; run this explicitly with: psql -f migrations/002_route_plan_loads_down.sql

ALTER TABLE route_plans DROP COLUMN loads;

DELETE FROM schema_migrations WHERE version = '002_route_plan_loads.sql';

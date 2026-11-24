DROP TABLE IF EXISTS outbox;
DROP TRIGGER IF EXISTS admin_actions_guard ON admin_actions;
DROP FUNCTION IF EXISTS admin_actions_guard();
DROP TABLE IF EXISTS admin_actions;

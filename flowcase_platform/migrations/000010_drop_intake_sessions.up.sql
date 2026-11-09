-- Conversational case intake moved to Universal Case Workspace, which stores its own intake
-- sessions; Flowcase no longer uses this table.
DROP TABLE IF EXISTS intake_sessions;

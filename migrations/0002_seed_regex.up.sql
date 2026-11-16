-- Placeholder pattern only, matching the one hardcoded in
-- internal/ports/stub.RegexSource (url:username:password). Real
-- predefined patterns (README section 1.1) are a separate
-- content-authoring concern from this migration mechanism - replace
-- or supplement this row once real patterns are available.
INSERT INTO regex (name, pattern, is_active) VALUES
    ('placeholder-url-user-pass', '^(?P<url>\S+):(?P<username>\S+):(?P<password>\S+)$', TRUE);

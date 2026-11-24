-- Predefined regex patterns (README section 1.1). Patterns use the
-- named capture groups required by internal/processing/extract
-- (url, username, password); group order 1=url, 2=username, 3=password.
-- SQL literals store Go regex syntax verbatim, so \s, \d, \w etc.
-- keep their single-backslash meaning (standard_conforming_strings=on).
INSERT INTO regex (name, pattern, is_active) VALUES
    ('url_user_pass', 'URL: (?P<url>.+)\sUSER: (?P<username>.+)\sPASS: (?P<password>.+)', TRUE),
    ('url_username_password', 'URL: (?P<url>.+)\sUsername: (?P<username>.+)\sPassword: (?P<password>.+)', TRUE),
    ('url_with_port_username_password', '^(?:https?:\/\/)?(?P<url>[\w.-]+\.[a-z]{2,}(?::\d+)?(?:\/\S*?)?):(?P<username>[^:\s]+@[^:\s]+|[^:\s]+):(?P<password>[^:\s]+)$', TRUE),
    ('url_with_port_username_password_pipe', '^(?:https?:\/\/)?(?P<url>[\w.-]+\.[a-z]{2,}(?::\d+)?(?:\/\S*?)?)\|(?P<username>[^|]+)\|(?P<password>[^|]+)$', TRUE),
    ('url_login_password', 'URL: (?P<url>.+)\sLOGIN: (?P<username>.+)\sPassword: (?P<password>.+)', TRUE),
    ('host_login_password', 'Host: (?P<url>.+)\sLogin: (?P<username>.+)\sPassword: (?P<password>.+)', TRUE),
    ('host_username_password', 'Host: (?P<url>.+)\sUsername: (?P<username>.+)\sPassword: (?P<password>.+)', TRUE);

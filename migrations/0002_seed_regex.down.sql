DELETE FROM regex WHERE name IN (
    'url_user_pass',
    'url_username_password',
    'url_with_port_username_password',
    'url_with_port_username_password_pipe',
    'url_login_password',
    'host_login_password',
    'host_username_password'
);

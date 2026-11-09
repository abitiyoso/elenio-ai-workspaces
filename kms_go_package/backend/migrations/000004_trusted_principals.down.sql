DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM tenant_principals) THEN
        RAISE EXCEPTION 'refusing to remove provisioned tenant principals';
    END IF;
END
$$;

DROP TABLE tenant_principals;

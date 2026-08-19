ALTER TABLE works
    DROP CONSTRAINT works_lifecycle_check;

ALTER TABLE works
    ADD CONSTRAINT works_lifecycle_check CHECK (lifecycle = 'open');

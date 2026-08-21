ALTER TABLE external_login_transactions
ADD COLUMN invitation_id uuid,
ADD constraint external_login_invitation_purpose_check CHECK (
    invitation_id IS NULL OR purpose = 'login'
);

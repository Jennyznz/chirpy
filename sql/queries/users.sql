-- name: CreateUser :one
INSERT INTO users (id, created_at, updated_at, email, hashed_password)
VALUES (
    gen_random_uuid(),
    NOW(),
    NOW(),
    $1,
    $2
)
RETURNING *;

-- name: ResetUsers :exec
DELETE FROM users;

-- name: Login :one
SELECT * FROM users
WHERE email = $1;

-- name: UpdateEmailAndPassword :one
UPDATE users
SET email = $2, hashed_password =$3
WHERE id = $1
RETURNING id, created_at, updated_at, email;
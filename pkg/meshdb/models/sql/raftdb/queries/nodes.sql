-- name: GetNodeCount :one
SELECT COUNT(*) AS count FROM nodes;

-- name: NodeExists :one
SELECT 1 FROM nodes WHERE id = ?;

-- name: EitherNodeExists :one
SELECT 1 FROM nodes WHERE id = ? OR id = ?;

-- name: InsertNode :one
INSERT INTO nodes (
    id,
    public_key,
    primary_endpoint,
    additional_endpoints,
    zone_awareness_id,
    network_ipv6,
    grpc_port,
    raft_port,
    wireguard_port,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
    public_key = EXCLUDED.public_key,
    primary_endpoint = EXCLUDED.primary_endpoint,
    additional_endpoints = EXCLUDED.additional_endpoints,
    zone_awareness_id = EXCLUDED.zone_awareness_id,
    network_ipv6 = EXCLUDED.network_ipv6,
    grpc_port = EXCLUDED.grpc_port,
    raft_port = EXCLUDED.raft_port,
    wireguard_port = EXCLUDED.wireguard_port,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: DeleteNode :exec
DELETE FROM nodes WHERE id = ?;

-- name: GetNode :one
SELECT
    nodes.id AS id,
    nodes.public_key AS public_key,
    nodes.primary_endpoint AS primary_endpoint,
    nodes.additional_endpoints AS additional_endpoints,
    nodes.zone_awareness_id AS zone_awareness_id,
    nodes.grpc_port AS grpc_port,
    nodes.raft_port AS raft_port,
    nodes.wireguard_port AS wireguard_port,
    nodes.network_ipv6 AS network_ipv6,
    COALESCE(leases.ipv4, '') AS private_address_v4,
    nodes.updated_at AS updated_at,
    nodes.created_at AS created_at
FROM nodes 
LEFT OUTER JOIN leases ON nodes.id = leases.node_id
WHERE nodes.id = ?;

-- name: ListNodeIDs :many
SELECT nodes.id AS id FROM nodes;

-- name: ListNodes :many
SELECT
    nodes.id AS id,
    nodes.public_key AS public_key,
    nodes.primary_endpoint AS primary_endpoint,
    nodes.additional_endpoints AS additional_endpoints,
    nodes.zone_awareness_id AS zone_awareness_id,
    nodes.grpc_port AS grpc_port,
    nodes.raft_port AS raft_port,
    nodes.wireguard_port AS wireguard_port,
    nodes.network_ipv6 AS network_ipv6,
    COALESCE(leases.ipv4, '') AS private_address_v4,
    nodes.updated_at AS updated_at,
    nodes.created_at AS created_at
FROM nodes 
LEFT OUTER JOIN leases ON nodes.id = leases.node_id;

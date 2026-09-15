-- 齿轮感应淬火复盘：ClickHouse 高频轨迹存储
-- 设计要点：
--   * 按批次分区（toYYYYMM(ts)），便于按月份归档/删除；
--   * 主键 (batch_id, ts) 保证批次内时间有序，回放查询走范围扫描；
--   * 高频采样（200Hz+）用 DateTime64(3) 毫秒精度，时钟漂移由复盘服务校正，
--     原始时间戳原样保留（可追溯）。

CREATE DATABASE IF NOT EXISTS hardening;

-- 高频功率/频率采样
CREATE TABLE IF NOT EXISTS hardening.hf_power (
    batch_id   String,
    ts         DateTime64(3, 'UTC'),
    power_kw   Float32,
    freq_khz   Float32
) ENGINE = MergeTree
PARTITION BY toYYYYMM(ts)
ORDER BY (batch_id, ts)
TTL ts + INTERVAL 2 YEAR;

-- 高频轴位置采样（Z 扫描轴 + 工件旋转角）
CREATE TABLE IF NOT EXISTS hardening.hf_axis (
    batch_id   String,
    ts         DateTime64(3, 'UTC'),
    z_mm       Float32,
    theta_deg  Float32
) ENGINE = MergeTree
PARTITION BY toYYYYMM(ts)
ORDER BY (batch_id, ts)
TTL ts + INTERVAL 2 YEAR;

-- 机床事件（NATS 消费后落库，参考时钟）
CREATE TABLE IF NOT EXISTS hardening.machine_events (
    batch_id   String,
    ts         DateTime64(3, 'UTC'),
    source     LowCardinality(String),
    type       LowCardinality(String),
    payload    Map(String, String)
) ENGINE = MergeTree
PARTITION BY toYYYYMM(ts)
ORDER BY (batch_id, ts)
TTL ts + INTERVAL 2 YEAR;

-- 红外测温摘要（gRPC 拉取后归档）
CREATE TABLE IF NOT EXISTS hardening.pyro_summary (
    batch_id   String,
    ts         DateTime64(3, 'UTC'),
    channel    LowCardinality(String),
    temp_c     Float32,
    emissivity Float32
) ENGINE = MergeTree
PARTITION BY toYYYYMM(ts)
ORDER BY (batch_id, ts)
TTL ts + INTERVAL 2 YEAR;

-- 标注候选（复盘输出，人工复核后更新 review_status）
CREATE TABLE IF NOT EXISTS hardening.annotations (
    batch_id   String,
    t_start    DateTime64(3, 'UTC'),
    t_end      DateTime64(3, 'UTC'),
    type       LowCardinality(String),
    severity   Float32,
    message    String,
    evidence   Map(String, Float64),
    review_status LowCardinality(String) DEFAULT 'candidate', -- candidate/confirmed/rejected
    reviewer   String DEFAULT ''
) ENGINE = MergeTree
PARTITION BY toYYYYMM(t_start)
ORDER BY (batch_id, t_start)
TTL t_start + INTERVAL 2 YEAR;

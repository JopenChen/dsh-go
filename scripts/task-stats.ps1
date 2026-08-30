# ============================================================================
# task-stats.ps1
# 用途：重新统计 docs/tasks.json 中各项任务的实际状态计数，供实现 Agent
#       在更新 meta.taskCountByStatus 时使用（保证数字与实际 status 字段一致）。
# 用法：powershell -File scripts/task-stats.ps1
# ============================================================================
$ErrorActionPreference = "Stop"

# 定位仓库根目录（脚本位于 <root>/scripts 下）
$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$jsonPath = Join-Path $root "docs\tasks.json"

$json = Get-Content -Path $jsonPath -Raw -Encoding UTF8 | ConvertFrom-Json

# 初始化状态计数器
$counts = @{ pending = 0; in_progress = 0; completed = 0; blocked = 0; deferred = 0; skipped = 0 }

foreach ($t in $json.tasks) {
    $s = $t.status
    if (-not $counts.ContainsKey($s)) { $counts[$s] = 0 }
    $counts[$s]++
}

Write-Host "=== docs/tasks.json 任务状态统计 ==="
Write-Host ("total      : {0}" -f $json.tasks.Count)
Write-Host ("pending    : {0}" -f $counts['pending'])
Write-Host ("in_progress: {0}" -f $counts['in_progress'])
Write-Host ("completed  : {0}" -f $counts['completed'])
Write-Host ("blocked    : {0}" -f $counts['blocked'])
Write-Host ("deferred   : {0}" -f $counts['deferred'])
Write-Host ("skipped    : {0}" -f $counts['skipped'])

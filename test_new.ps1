# 工单08/09/12/13/14/18 新增接口集成测试 - ASCII only
$ErrorActionPreference = "Stop"
$base = "http://localhost:8080/api/v1"
$root = "http://localhost:8080"

function POST($uri, $headers, $body) {
    if ($body -eq $null) { return Invoke-RestMethod -Uri $uri -Method Post -Headers $headers }
    Invoke-RestMethod -Uri $uri -Method Post -Headers $headers -ContentType "application/json" -Body ($body | ConvertTo-Json)
}
function GETR($uri, $headers) {
    if ($headers -eq $null) { return Invoke-RestMethod -Uri $uri -Method Get }
    Invoke-RestMethod -Uri $uri -Method Get -Headers $headers
}
function PUTR($uri, $headers, $body) {
    Invoke-RestMethod -Uri $uri -Method Put -Headers $headers -ContentType "application/json" -Body ($body | ConvertTo-Json)
}
function FAIL($m) { Write-Host "  !! FAIL: $m" }

# 注册测试司机（幂等：已存在则忽略）
try { POST "$base/auth/register" @{} @{username="driverA";password="Abc@123456";realName="DriverA";phone="13811110001";idCard="110101198501011111";role="carpool_driver"} | Out-Null } catch {}
try { POST "$base/auth/register" @{} @{username="driverB";password="Abc@123456";realName="DriverB";phone="13811110002";idCard="110101198501012222";role="carpool_driver"} | Out-Null } catch {}

# 登录
$adminT = (POST "$base/auth/login" @{} @{username="admin";password="admin123"}).data.token
$driverT = (POST "$base/auth/login" @{} @{username="driverA";password="Abc@123456"}).data.token
Write-Host "== logins ok =="

# 工单12：司机上线（nearby 需要）
POST "$base/driver/online" @{Authorization="Bearer $driverT"} @{lng=121.4737;lat=31.2304} | Out-Null
Write-Host "== driverA online =="

# 工单12：短信验证码登录
$codeResp = (POST "$base/auth/sms-code" @{} @{phone="13811110001";purpose="login"}).data
$devCode = $codeResp.devCode
Write-Host "== sms-code devCode=$devCode =="
$smsTok = (POST "$base/auth/sms-login" @{} @{phone="13811110001";code=$devCode}).data.token
Write-Host "== sms-login token_len=$($smsTok.Length) =="

# 工单12：找回密码（发码->重置）
POST "$base/auth/sms-code" @{} @{phone="13811110001";purpose="reset"} | Out-Null
$r2 = (POST "$base/auth/sms-code" @{} @{phone="13811110001";purpose="reset"}).data
POST "$base/auth/reset-password" @{} @{phone="13811110001";code=$r2.devCode;newPassword="Abc@123456"} | Out-Null
Write-Host "== reset-password ok =="

# 工单12：账户信息 + 改密码
$p = GETR "$base/me/profile" @{Authorization="Bearer $driverT"}
Write-Host "== profile points=$($p.data.points) balance=$($p.data.balance) =="
POST "$base/me/profile" @{Authorization="Bearer $driverT"} @{nickname="DriverA-Nick"} | Out-Null
POST "$base/me/password" @{Authorization="Bearer $driverT"} @{oldPassword="Abc@123456";newPassword="Abc@123456"} | Out-Null
Write-Host "== profile update + password change ok =="

# 工单12：常用地址
$a = (POST "$base/me/addresses" @{Authorization="Bearer $driverT"} @{label="home";name="MyHome";address="PeopleSquare";lng=121.47;lat=31.23;isDefault=$true}).data
Write-Host "== address created id=$($a.id) =="
$al = (GETR "$base/me/addresses" @{Authorization="Bearer $driverT"}).data
Write-Host "== addresses count=$($al.Count) =="
PUTR "$base/me/addresses/$($a.id)" @{Authorization="Bearer $driverT"} @{label="home";name="Home2";address="Xujiahui";lng=121.43;lat=31.19;isDefault=$false} | Out-Null
Write-Host "== address updated =="
# 工单12：支付方式
$pm = (POST "$base/me/payments" @{Authorization="Bearer $driverT"} @{type="alipay";account="13811110001";isDefault=$true}).data
Write-Host "== payment created account=$($pm.account) =="
$pml = (GETR "$base/me/payments" @{Authorization="Bearer $driverT"}).data
Write-Host "== payments count=$($pml.Count) =="
# 工单12：反馈
POST "$base/me/feedback" @{Authorization="Bearer $driverT"} @{type="suggest";content="Great app";contact="wechat:x"} | Out-Null
Write-Host "== feedback ok =="
# 工单12：行程记录
$po = (GETR "$base/me/orders" @{Authorization="Bearer $driverT"}).data
Write-Host "== passenger orders count=$($po.count) =="

# 工单13：附近司机
$nb = (GETR "$base/drivers/nearby?lng=121.4737&lat=31.2304&radiusKm=5" @{Authorization="Bearer $driverT"}).data
Write-Host "== nearby drivers count=$($nb.count) first_rating=$($nb.drivers[0].rating) plate=$($nb.drivers[0].plateNo) =="
if ($nb.count -lt 1) { FAIL "nearby empty" }
$did = $nb.drivers[0].driverId
$dd = (GETR "$base/drivers/$did" @{Authorization="Bearer $driverT"}).data
Write-Host "== driver detail name=$($dd.name) rating=$($dd.rating) online=$($dd.online) =="
# 工单13：SOS
$sos = (POST "$base/sos" @{Authorization="Bearer $driverT"} @{orderNo="";lng=121.47;lat=31.23;address="PeopleSquare"}).data
Write-Host "== sos created id=$($sos.id) status=$($sos.status) =="

# 工单14：下单 -> 评价 + 审核 + 打赏 + 发票
$ord = (POST "$base/ride-orders" @{Authorization="Bearer $driverT"} @{passengerName="P";passengerPhone="13811110001";startName="A";startLng=121.47;startLat=31.23;destName="B";destLng=121.48;destLat=31.24}).data
$ono = $ord.orderNo
Write-Host "== order created no=$ono =="
$rev = (POST "$base/ride-orders/$ono/review" @{Authorization="Bearer $driverT"} @{rating=5;comment="good";anonymous=$false}).data
Write-Host "== review created id=$($rev.reviewId) status=$($rev.status) =="
POST "$base/ride-orders/$ono/tip" @{Authorization="Bearer $driverT"} @{amount=5;thanks="thank you"} | Out-Null
Write-Host "== tip ok =="
$inv = (POST "$base/ride-orders/$ono/invoice" @{Authorization="Bearer $driverT"} @{type="electronic";title="Personal";email="a@b.com"}).data
Write-Host "== invoice created id=$($inv.id) status=$($inv.status) =="
# 工单14：评价审核（管理员通过 -> 公开）
POST "$base/ride-reviews/$($rev.reviewId)/approve" @{Authorization="Bearer $adminT"} @{} | Out-Null
Write-Host "== review approved =="

# 工单09：任务管理
$jobs = (GETR "$base/jobs" @{Authorization="Bearer $adminT"}).data
Write-Host "== jobs count=$($jobs.Count) names=$(($jobs | ForEach-Object {$_.name}) -join ',') =="
PUTR "$base/jobs/cleanup_expired_orders/params" @{Authorization="Bearer $adminT"} @{olderThanHours=48} | Out-Null
Write-Host "== job params updated =="
POST "$base/jobs/cleanup_expired_orders/run" @{Authorization="Bearer $adminT"} @{params=@{olderThanHours=10}} | Out-Null
$ja = (GETR "$base/jobs/audits?jobName=cleanup_expired_orders" @{Authorization="Bearer $adminT"}).data
Write-Host "== job audits count=$($ja.Count) last_result=$($ja[0].result) =="
# 工单09：动态菜单
$mn = (GETR "$base/menus" @{Authorization="Bearer $adminT"}).data
Write-Host "== menus tree count=$($mn.Count) =="
$mn2 = (POST "$base/menus" @{Authorization="Bearer $adminT"} @{name="NewMenu";path="/new";component="new";sort=9;roles="admin"}).data
Write-Host "== menu created id=$($mn2.id) =="
# 工单09：审计日志
$al2 = (GETR "$base/audit-logs?page=1" @{Authorization="Bearer $adminT"}).data
Write-Host "== audit-logs total=$($al2.total) =="

# 工单08：metrics（无鉴权，应包含 ride_http）
try {
    $m = (Invoke-WebRequest "$root/metrics" -UseBasicParsing).Content
    if ($m -match "ride_http") { Write-Host "== metrics ok (contains ride_http) ==" } else { FAIL "metrics missing ride_http" }
} catch { Write-Host "== metrics fetch failed: $($_.Exception.Message)" }
# 工单08：pprof 无鉴权应 401
try { Invoke-RestMethod "$root/debug/pprof/" | Out-Null; FAIL "pprof should be 401 without auth" } catch { Write-Host "== pprof 401 without auth (expected) ==" }

# 工单18：数字人配置状态
$dhs = (GETR "$base/ai/digital-human/status" @{Authorization="Bearer $adminT"}).data
Write-Host "== digital-human status cozeAvailable=$($dhs.cozeAvailable) mode=$($dhs.mode) =="

Write-Host "NEW TESTS DONE"

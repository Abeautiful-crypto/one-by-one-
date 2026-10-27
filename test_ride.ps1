# Ride-hailing integration tests for new work orders (10/11/15/16/17/19) - ASCII only
$ErrorActionPreference = "Stop"
$base = "http://localhost:8080/api/v1"

function POST($uri, $headers, $body) {
    Invoke-RestMethod -Uri $uri -Method Post -Headers $headers -ContentType "application/json" -Body ($body | ConvertTo-Json)
}
function PUTR($uri, $headers, $body) {
    Invoke-RestMethod -Uri $uri -Method Put -Headers $headers -ContentType "application/json" -Body ($body | ConvertTo-Json)
}
function GETR($uri, $headers) { Invoke-RestMethod -Uri $uri -Method Get -Headers $headers }
function Fail($msg) { Write-Host "  !! FAIL: $msg" }

# Login & accounts
$ta = (POST "$base/auth/login" @{} @{username="admin";password="admin123"}).data.token
try { POST "$base/auth/register" @{} @{username="driverA";password="Abc@123456";realName="DriverA";phone="13811110001";idCard="110101198501011111";role="carpool_driver"} | Out-Null } catch {}
try { POST "$base/auth/register" @{} @{username="driverB";password="Abc@123456";realName="DriverB";phone="13811110002";idCard="110101198501012222";role="carpool_driver"} | Out-Null } catch {}
$tD1 = (POST "$base/auth/login" @{} @{username="driverA";password="Abc@123456"}).data.token
$tD2 = (POST "$base/auth/login" @{} @{username="driverB";password="Abc@123456"}).data.token
Write-Host "== ready: admin/driverA/driverB =="

# 1. Drivers online (W15/17: Redis status + GEO)
$onA = (POST "$base/driver/online" @{Authorization="Bearer $tD1"} @{lng=121.4737;lat=31.2304}).data
$onB = (POST "$base/driver/online" @{Authorization="Bearer $tD2"} @{lng=121.4800;lat=31.2350}).data
Write-Host "== online: A=$($onA.status) B=$($onB.status) =="

# 2. Create order + idempotency (W11)
$orderBody = @{passengerName="PassengerC";passengerPhone="13822220003";startName="PeopleSquare";startLng=121.4737;startLat=31.2304;destName="HongqiaoAirport";destLng=121.3363;destLat=31.1979;vehicleType="economy"}
$o1 = (POST "$base/ride-orders" @{Authorization="Bearer $ta";"X-Idempotency-Key"="IDEM001"} $orderBody).data
$o1b = (POST "$base/ride-orders" @{Authorization="Bearer $ta";"X-Idempotency-Key"="IDEM001"} $orderBody).data
Write-Host "== order: no=$($o1.orderNo) status=$($o1.status) estimate=$($o1.estimateFare) dist=$([math]::Round($o1.distanceKm,2))km"
Write-Host "   idempotent-same-order: $($o1b.orderNo -eq $o1.orderNo)"
if ($o1b.orderNo -ne $o1.orderNo) { Fail "idempotency broken" }

# 3. Hall list (W17)
$hall = (GETR "$base/orders/list?lng=121.4737&lat=31.2304&sortBy=distance" @{Authorization="Bearer $tD1"}).data
Write-Host "== hall: total=$($hall.total) first.dist=$($hall.list[0].distanceKmFromDriver)km fare=$($hall.list[0].estimateFare)"
if ($hall.total -lt 1) { Fail "hall empty" }

# 4. Accept race (W15/17)
$acc = (POST "$base/ride-orders/accept" @{Authorization="Bearer $tD1"} @{orderNo=$o1.orderNo}).data
Write-Host "== A accept: status=$($acc.status) driver=$($acc.driverName) plate=$($acc.vehiclePlate)"
try {
    POST "$base/ride-orders/accept" @{Authorization="Bearer $tD2"} @{orderNo=$o1.orderNo} | Out-Null
    Fail "B accepted unexpectedly (race broken)"
} catch {
    $e = $_.ErrorDetails.Message | ConvertFrom-Json
    Write-Host "== B accept-again expected fail: $($e.msg)"
}

# 5. Trip flow (W15 pickup + W16 trip/bill)
$no = $o1.orderNo
$s1 = (POST "$base/ride-orders/$no/arrive" @{Authorization="Bearer $tD1"} @{}).data
Write-Host "== arrive: $($s1.status)"
$s2 = (POST "$base/ride-orders/$no/pickup" @{Authorization="Bearer $tD1"} @{}).data
Write-Host "== pickup: $($s2.status)"
$s3 = (POST "$base/ride-orders/$no/start" @{Authorization="Bearer $tD1"} @{startAddr="PeopleSquare Exit1"}).data
Write-Host "== start: $($s3.status)"
Start-Sleep -Seconds 2
$fin = (POST "$base/ride-orders/$no/finish" @{Authorization="Bearer $tD1"} @{endAddr="HongqiaoT2";tollFare=8}).data
$b = $fin.bill
Write-Host "== finish: order=$($fin.order.status) mileage=$($fin.order.mileageKm)km"
Write-Host "   bill: base=$($b.baseFare) mileage=$($b.mileageFare) duration=$($b.durationFare) toll=$($b.tollFare) night=$($b.nightExtra) total=$($b.total)"
$cb = (POST "$base/ride-orders/$no/confirm-bill" @{Authorization="Bearer $tD1"} @{}).data
Write-Host "== confirm-bill: total=$($cb.total)"
$det = (GETR "$base/ride-orders/$no" @{Authorization="Bearer $ta"}).data
Write-Host "== detail: audits=$($det.audits.Count) hasBill=$($det.hasBill) dispatchTop=$($det.dispatchTop.Count)"

# 6. Cancel compensation (W16)
$o2 = (POST "$base/ride-orders" @{Authorization="Bearer $ta"} ($orderBody + @{idempotencyKey="IDEM002"})).data
POST "$base/ride-orders/accept" @{Authorization="Bearer $tD2"} @{orderNo=$o2.orderNo} | Out-Null
$cancel = (POST "$base/ride-orders/$($o2.orderNo)/cancel?by=passenger" @{Authorization="Bearer $ta"} @{reason="plan changed";idempotencyKey="CANCEL-002"}).data
Write-Host "== cancel: status=$($cancel.order.status) compensation=$($cancel.compensation) (expect 5)"
$c2 = (POST "$base/ride-orders/$($o2.orderNo)/cancel?by=passenger" @{Authorization="Bearer $ta"} @{reason="plan changed";idempotencyKey="CANCEL-002"}).data
Write-Host "   idempotent-cancel: $($c2.order.orderNo -eq $cancel.order.orderNo)"

# 7. Update destination (W16)
$o3 = (POST "$base/ride-orders" @{Authorization="Bearer $ta"} ($orderBody + @{idempotencyKey="IDEM003"})).data
$upd = (PUTR "$base/ride-orders/$($o3.orderNo)/destination" @{Authorization="Bearer $ta"} @{destName="PudongAirport";lng=121.8056;lat=31.1434}).data
Write-Host "== update-dest: $($upd.destName) newEstimate=$($upd.estimateFare) newDist=$([math]::Round($upd.distanceKm,2))km"

# 8. AI assistant (W10)
$rr = (POST "$base/ai/rain-route" @{Authorization="Bearer $ta"} @{origin=@{lat=31.2304;lng=121.4737};destination=@{lat=31.1979;lng=121.3363};avoid=@("flooded_roads","underpasses");preference="safest";rainfall="heavy"}).data
Write-Host "== rain-route FC: routes=$($rr.routes.Count) recommend=$($rr.recommended.name) risk=$($rr.recommended.riskLevel) eta=$($rr.recommended.durationMin)min"
POST "$base/ai/weather-alert" @{Authorization="Bearer $ta"} @{city="Shanghai";level="orange";type="rainstorm";title="Rainstorm Orange Alert";impact="wait+8min fare+15%";activeHours=4} | Out-Null
$wa = (GETR "$base/ai/weather-alert?city=Shanghai" @{Authorization="Bearer $ta"}).data
Write-Host "== weather-alert: active=$($wa.active) level=$($wa.alert.level)"
$sh = (GETR "$base/ai/shelters?lng=121.4737&lat=31.2304" @{Authorization="Bearer $ta"}).data
Write-Host "== shelters: count=$($sh.Count) first=$($sh[0].name) coupon=$($sh[0].couponValue)"
$fr = (POST "$base/ai/flood-report" @{Authorization="Bearer $ta"} @{roadName="Zhonghuan Inner";depthCm=15;lng=121.47;lat=31.23;photoUrl="s3://flood/001.jpg"}).data
Write-Host "== flood-report: road=$($fr.report.roadName) depth=$($fr.report.depthCm)cm affected=$($fr.affectedOrders) coupon=$($fr.coupon)"

# 9. Dispatch config & rollback (W11)
Write-Host "DIAG token=$($ta.Substring(0,20)) len=$($ta.Length)"
$pre = (GETR "$base/auth/me" @{Authorization="Bearer $ta"}).data
Write-Host "DIAG me roles=[$(($pre.roles) -join ',')] user=$($pre.username)"
$cfg = (POST "$base/dispatch-config/Shanghai" @{Authorization="Bearer $ta"} @{WGeo=0.4;WRating=0.25;WTimeWindow=0.15;WAcceptRate=0.2;SearchRadiusKm=6;DayStart=7;DayEnd=22;DayTimeoutSec=45;NightTimeoutSec=90;DriverPool=""}).data
Write-Host "== dispatch-config: city=$($cfg.city) version=$($cfg.version)"
$cfg2 = (POST "$base/dispatch-config/Shanghai" @{Authorization="Bearer $ta"} @{WGeo=0.5;WRating=0.2;WTimeWindow=0.15;WAcceptRate=0.15;SearchRadiusKm=8;DayStart=7;DayEnd=22;DayTimeoutSec=30;NightTimeoutSec=60;DriverPool=""}).data
Write-Host "   second save version=$($cfg2.version) radius=$($cfg2.searchRadiusKm)"
$rb = (POST "$base/dispatch-config/Shanghai/rollback" @{Authorization="Bearer $ta"} @{}).data
Write-Host "   after rollback version=$($rb.version) radius=$($rb.searchRadiusKm) (expect 6)"

# 10. Trace report & replay (W11)
POST "$base/ride-orders/$($o3.orderNo)/trace" @{Authorization="Bearer $ta"} @{lng=121.474;lat=31.231;speed=30} | Out-Null
POST "$base/ride-orders/$($o3.orderNo)/trace" @{Authorization="Bearer $ta"} @{lng=121.470;lat=31.228;speed=42} | Out-Null
$tr = (GETR "$base/ride-orders/$($o3.orderNo)/trace" @{Authorization="Bearer $ta"}).data
Write-Host "== trace: points=$($tr.points.Count) events=$($tr.events.Count)"

# 11. Order list aggregate (W11)
$ls = (GETR "$base/ride-orders?status=cancelled" @{Authorization="Bearer $ta"}).data
Write-Host "== list-aggregate: total=$($ls.total) stats=[$(($ls.stats.PSObject.Properties.Name) -join ',')]"
Write-Host "ALL REST TESTS DONE"

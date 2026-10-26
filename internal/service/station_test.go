package service

import "testing"

func TestSortStationsNoDup(t *testing.T) {
	inputs := []StationInput{
		{StationName: "中关村", StopType: "up", Minutes: 0},
		{StationName: "北苑", StopType: "down", Minutes: 20},
		{StationName: "立水桥", StopType: "down", Minutes: 35},
		{StationName: "望京", StopType: "down", Minutes: 50},
	}
	out, err := SortStations(inputs)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	for _, s := range out {
		t.Logf("seq-ready: %s %s %d", s.StationName, s.StopType, s.Minutes)
	}
}

func TestSortStationsDup(t *testing.T) {
	inputs := []StationInput{
		{StationName: "中关村", StopType: "up", Minutes: 0},
		{StationName: "中关村", StopType: "up", Minutes: 5}, // 同名同类型重复
	}
	_, err := SortStations(inputs)
	if err == nil {
		t.Fatalf("expected dup error")
	}
	t.Logf("got expected err: %v", err)
}

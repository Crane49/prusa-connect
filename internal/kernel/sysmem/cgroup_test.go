package sysmem

import (
	"errors"
	"testing"
)

func TestCgroupLimit(t *testing.T) {
	tests := []struct {
		name  string
		self  string
		files map[string]string
		want  int64
		ok    bool
	}{
		{
			name: "v2 ancestor limit",
			self: "0::/system.slice/pcl.service\n",
			files: map[string]string{
				"/sys/fs/cgroup/system.slice/pcl.service/memory.max": "max\n",
				"/sys/fs/cgroup/system.slice/memory.max":             "536870912\n",
			},
			want: 536870912, ok: true,
		},
		{
			name: "v2 no limit",
			self: "0::/a/b\n",
			files: map[string]string{
				"/sys/fs/cgroup/a/b/memory.max": "max\n",
				"/sys/fs/cgroup/a/memory.max":   "max\n",
				"/sys/fs/cgroup/memory.max":     "max\n",
			},
		},
		{
			name:  "v2 container namespace",
			self:  "0::/\n",
			files: map[string]string{"/sys/fs/cgroup/memory.max": "2147483648\n"},
			want:  2147483648, ok: true,
		},
		{
			name:  "v1 unlimited",
			self:  "4:memory:/docker/x\n",
			files: map[string]string{"/sys/fs/cgroup/memory/docker/x/memory.limit_in_bytes": "9223372036854771712\n"},
		},
		{
			name:  "v1 limited",
			self:  "4:cpu,memory:/docker/x\n",
			files: map[string]string{"/sys/fs/cgroup/memory/docker/x/memory.limit_in_bytes": "268435456\n"},
			want:  268435456, ok: true,
		},
		{name: "unreadable", self: "0::/a\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			read := func(p string) ([]byte, error) {
				if s, ok := tc.files[p]; ok {
					return []byte(s), nil
				}
				return nil, errors.New("not found")
			}
			got, ok := cgroupLimit(tc.self, read)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("got %d, %v; want %d, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

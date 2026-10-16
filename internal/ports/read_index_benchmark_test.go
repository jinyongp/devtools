package ports

import (
	"fmt"
	"testing"
)

func BenchmarkRegistryValidation(b *testing.B) {
	for _, count := range []int{200, 2000} {
		b.Run(fmt.Sprintf("instances_%d", count), func(b *testing.B) {
			st := empty()
			for n := 0; n < count; n++ {
				i := Instance{ID: fmt.Sprintf("%032x", n+1), Profile: "bench", Directory: fmt.Sprintf("/bench/%d", n)}
				st.Instances = append(st.Instances, i)
				for service := 0; service < 3; service++ {
					st.Assignments = append(st.Assignments, Assignment{Instance: i, Name: fmt.Sprintf("service%d", service), Port: 20000 + n*3 + service})
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				if !st.valid() {
					b.Fatal("invalid fixture")
				}
			}
		})
	}
}

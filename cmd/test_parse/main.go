package main

import (
	"fmt"

	pkgutils "example.com/rbmq-demo/pkg/utils"
)

func main() {
	ttls, err := pkgutils.ParseInts("range(1;10)")
	fmt.Println(ttls, err)

	ttls, err = pkgutils.ParseInts("range(1;10;1)")
	fmt.Println(ttls, err)

	ttls, err = pkgutils.ParseInts("range(1;10;2)")
	fmt.Println(ttls, err)

	ttls, err = pkgutils.ParseInts("1,2,3,4,5,6,7,8,9,10")
	fmt.Println(ttls, err)

	ttls, err = pkgutils.ParseInts("64")
	fmt.Println(ttls, err)
}

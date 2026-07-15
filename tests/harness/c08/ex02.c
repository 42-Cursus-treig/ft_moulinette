#include "ft_abs.h"
#include <stdio.h>

int	main(void)
{
	int	a;
	int	b;
	int	c;

	a = ABS(-42);
	b = ABS(42);
	c = ABS(0);
	printf("%d\n%d\n%d\n", a, b, c);
	return (0);
}

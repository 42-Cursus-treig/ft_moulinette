#include "ft.h"
#include <unistd.h>

int	main(void)
{
	int	a;
	int	b;

	ft_putstr("putstr;");
	ft_putchar('C');
	ft_putchar('\n');
	a = 3;
	b = 7;
	ft_swap(&a, &b);
	ft_putchar('0' + a);
	ft_putchar('0' + b);
	ft_putchar('\n');
	ft_putchar('0' + (ft_strlen("hello") == 5));
	ft_putchar('\n');
	ft_putchar('0' + (ft_strcmp("abc", "abc") == 0));
	ft_putchar('0' + (ft_strcmp("abc", "abd") < 0));
	ft_putchar('\n');
	return (0);
}

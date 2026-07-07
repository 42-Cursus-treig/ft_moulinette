#include <unistd.h>

void	ft_ft(int *nbr);

static void	put_int(int n)
{
	char	c;

	if (n < 0)
	{
		c = '-';
		write(1, &c, 1);
		n = -n;
	}
	if (n >= 10)
		put_int(n / 10);
	c = '0' + (n % 10);
	write(1, &c, 1);
}

int	main(void)
{
	int	n;

	n = 0;
	ft_ft(&n);
	put_int(n);
	return (0);
}

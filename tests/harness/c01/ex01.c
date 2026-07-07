#include <unistd.h>

void	ft_ultimate_ft(int *********nbr);

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
	int			n;
	int			*p1;
	int			**p2;
	int			***p3;
	int			****p4;
	int			*****p5;
	int			******p6;
	int			*******p7;
	int			********p8;
	int			*********p9;

	n = 0;
	p1 = &n;
	p2 = &p1;
	p3 = &p2;
	p4 = &p3;
	p5 = &p4;
	p6 = &p5;
	p7 = &p6;
	p8 = &p7;
	p9 = &p8;
	ft_ultimate_ft(p9);
	put_int(n);
	return (0);
}

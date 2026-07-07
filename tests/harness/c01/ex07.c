#include <stdlib.h>
#include <unistd.h>

void	ft_rev_int_tab(int *tab, int size);

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

int	main(int argc, char **argv)
{
	int	tab[32];
	int	size;
	int	i;

	size = argc - 1;
	if (size < 0 || size > 32)
		return (1);
	i = 0;
	while (i < size)
	{
		tab[i] = atoi(argv[i + 1]);
		i++;
	}
	ft_rev_int_tab(tab, size);
	i = 0;
	while (i < size)
	{
		if (i > 0)
			write(1, " ", 1);
		put_int(tab[i]);
		i++;
	}
	return (0);
}

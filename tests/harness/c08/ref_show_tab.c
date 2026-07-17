#include "ft_stock_str.h"
#include <unistd.h>

static void	ref_putstr(char *str)
{
	if (!str)
		return ;
	while (*str)
		write(1, str++, 1);
}

static void	ref_putnbr(int n)
{
	char			c;
	unsigned int	nb;

	if (n < 0)
	{
		write(1, "-", 1);
		nb = (unsigned int)(-n);
	}
	else
		nb = (unsigned int)n;
	if (nb >= 10)
		ref_putnbr((int)(nb / 10));
	c = '0' + (nb % 10);
	write(1, &c, 1);
}

void	ft_show_tab(struct s_stock_str *par)
{
	int	i;

	i = 0;
	while (par[i].str != 0)
	{
		ref_putstr(par[i].str);
		write(1, "\n", 1);
		ref_putnbr(par[i].size);
		write(1, "\n", 1);
		ref_putstr(par[i].copy);
		write(1, "\n", 1);
		i++;
	}
}

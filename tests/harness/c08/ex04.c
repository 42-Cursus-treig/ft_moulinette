#include "ft_stock_str.h"

int	main(int ac, char **av)
{
	t_stock_str	*tab;

	tab = ft_strs_to_tab(ac - 1, av + 1);
	if (!tab)
		return (1);
	ft_show_tab(tab);
	return (0);
}

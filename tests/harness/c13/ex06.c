#include "ft_btree.h"
#include <unistd.h>

int		btree_level_count(t_btree *root);
t_btree	*build_fixed_tree(void);
void	free_tree(t_btree *root);

static void	put_nbr(int n)
{
	char	buf[12];
	int		i;

	i = 0;
	if (n == 0)
		buf[i++] = '0';
	while (n > 0)
	{
		buf[i++] = '0' + (n % 10);
		n /= 10;
	}
	while (i--)
		write(1, &buf[i], 1);
}

int	main(int argc, char **argv)
{
	t_btree	*root;

	if (argc >= 2 && argv[1][0] == 'e')
	{
		put_nbr(btree_level_count(NULL));
		write(1, "\n", 1);
		return (0);
	}
	root = build_fixed_tree();
	put_nbr(btree_level_count(root));
	write(1, "\n", 1);
	free_tree(root);
	return (0);
}
